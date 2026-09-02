// Package parse turns a committed delivery into structured, searchable,
// safely renderable output.
//
// Everything this package writes is derived. The raw blob is the record, so a
// parse can fail, be retried, be re-run after a parser fix, or be abandoned
// entirely, and the message stays readable throughout. That is what lets the
// worker be permissive about malformed mail and strict about giving up.
package parse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/mimeparse"
	"github.com/ethchor/phenk/internal/sanitize"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
)

// MaxAttempts is how many times a delivery is parsed before it is left alone.
// A message that has failed three times is not going to succeed on the fourth,
// and the raw message stays readable regardless.
const MaxAttempts = 3

// ErrPermanent marks a failure that will not succeed on a retry, so the queue
// stops rather than burning attempts on a message that cannot be parsed.
var ErrPermanent = errors.New("parse: permanent failure")

// Parser turns one delivery into parsed output.
type Parser struct {
	db        *pg.DB
	blobs     blob.Store
	keyring   *crypto.Keyring
	sanitizer *sanitize.Sanitizer

	// EvaluateSPF is optional. When nil, SPF is recorded as none.
	EvaluateSPF SPFFunc

	maxAttachmentBytes int64
}

// Options configures a Parser.
type Options struct {
	// MaxAttachmentBytes caps a single extracted attachment. Anything larger
	// is recorded with its metadata but without a stored body, so a message
	// still lists what it carried.
	MaxAttachmentBytes int64
	EvaluateSPF        SPFFunc
}

// New builds a Parser.
func New(db *pg.DB, blobs blob.Store, keyring *crypto.Keyring, sanitizer *sanitize.Sanitizer, opts Options) *Parser {
	if opts.MaxAttachmentBytes <= 0 {
		opts.MaxAttachmentBytes = 25 << 20
	}
	return &Parser{
		db:                 db,
		blobs:              blobs,
		keyring:            keyring,
		sanitizer:          sanitizer,
		EvaluateSPF:        opts.EvaluateSPF,
		maxAttachmentBytes: opts.MaxAttachmentBytes,
	}
}

// Parse processes one delivery.
//
// A failure to parse is recorded on the delivery and returned, so the queue can
// retry; a failure that cannot succeed on a retry is wrapped in ErrPermanent.
// Either way the blob is kept: losing the raw message because the parser
// disagreed with it would be the one unrecoverable mistake here.
func (p *Parser) Parse(ctx context.Context, deliveryID core.UUID) error {
	delivery, err := pg.DeliveryByID(ctx, p.db, deliveryID)
	if err != nil {
		return fmt.Errorf("%w: loading delivery: %v", ErrPermanent, err)
	}
	if delivery.State == core.DeliveryParsed {
		// A retry that raced a success. Parsing again would be harmless but
		// pointless.
		return nil
	}

	identity, err := pg.IdentityByID(ctx, p.db, delivery.IdentityID)
	if err != nil {
		return fmt.Errorf("%w: loading identity: %v", ErrPermanent, err)
	}
	if len(identity.WrappedDataKey) == 0 {
		// The identity was purged while this job waited. There is no key left
		// to encrypt anything under, and nothing left worth reading.
		slog.Info("skipping parse for a purged identity", "delivery_id", deliveryID)
		return nil
	}
	dataKey, err := p.keyring.Unwrap(identity.ID, identity.WrappedDataKey)
	if err != nil {
		return fmt.Errorf("%w: unwrapping data key: %v", ErrPermanent, err)
	}
	defer dataKey.Destroy()

	raw, err := p.readBlob(ctx, delivery.BlobID)
	if err != nil {
		return err
	}

	parsed, attachments, err := p.parse(ctx, raw, dataKey)
	if err != nil {
		if markErr := pg.MarkDeliveryFailed(ctx, p.db, deliveryID); markErr != nil {
			slog.Error("recording parse failure", "delivery_id", deliveryID, "error", markErr)
		}
		return err
	}

	spf := core.AuthNone
	if p.EvaluateSPF != nil {
		spf = p.EvaluateSPF(delivery.ClientIP, delivery.HELO, delivery.EnvelopeFrom)
	}
	dkimResult, dkimDomains := verifyDKIM(raw)
	dmarc := deriveDMARC(parsed.message.From.Addr, spf, delivery.EnvelopeFrom, dkimResult, dkimDomains)

	parsed.stored.DeliveryID = deliveryID
	err = p.db.InTx(ctx, func(q pg.Querier) error {
		if err := pg.UpsertParsedMessage(ctx, q, parsed.stored, parsed.searchText); err != nil {
			return err
		}
		if err := pg.ReplaceAttachments(ctx, q, deliveryID, attachments); err != nil {
			return err
		}
		if err := pg.MarkDeliveryParsed(ctx, q, deliveryID, spf, dkimResult, dmarc); err != nil {
			return err
		}
		_, err := pg.AppendEvent(ctx, q, &delivery.IdentityID, core.EventMessageParsed, map[string]any{
			"delivery_id": deliveryID,
			"seq":         delivery.Seq,
			"subject":     parsed.stored.Subject,
			"from":        parsed.stored.FromAddr,
			"preview":     parsed.stored.Preview,
			"attachments": len(attachments),
			"spf":         string(spf),
			"dkim":        string(dkimResult),
			"dmarc":       string(dmarc),
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("parse: storing results for %s: %w", deliveryID, err)
	}

	slog.Info("parsed message", "delivery_id", deliveryID, "seq", delivery.Seq,
		"attachments", len(attachments), "spf", spf, "dkim", dkimResult, "dmarc", dmarc,
		"warnings", len(parsed.message.Warnings))
	return nil
}

func (p *Parser) readBlob(ctx context.Context, blobID core.UUID) ([]byte, error) {
	row, err := pg.BlobByID(ctx, p.db, blobID)
	if err != nil {
		return nil, fmt.Errorf("%w: loading blob row: %v", ErrPermanent, err)
	}
	sha, err := blob.SHA256FromBytes(row.SHA256)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPermanent, err)
	}
	rc, err := p.blobs.Get(ctx, sha)
	if err != nil {
		// A missing blob may be a storage hiccup, so this is worth retrying.
		return nil, fmt.Errorf("parse: reading blob %s: %w", sha, err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("parse: reading blob %s: %w", sha, err)
	}
	return raw, nil
}

// result carries what one parse produced, before it is written.
type result struct {
	message    *mimeparse.Message
	stored     *core.ParsedMessage
	searchText string
}

func (p *Parser) parse(ctx context.Context, raw []byte, dataKey *crypto.DataKey) (*result, []core.Attachment, error) {
	var attachments []core.Attachment

	msg, err := mimeparse.Parse(bytes.NewReader(raw), mimeparse.Options{
		Attachment: func(meta mimeparse.AttachmentMeta, body io.Reader) error {
			attachment, err := p.storeAttachment(ctx, meta, body, dataKey)
			if err != nil {
				return err
			}
			attachments = append(attachments, *attachment)
			return nil
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrPermanent, err)
	}

	// Sanitize before encrypting, so no code path can read unsanitized HTML
	// back out of the database. That is what makes invariant 8 structural
	// rather than a rule people have to remember.
	var htmlBody []byte
	if len(msg.HTML) > 0 {
		htmlBody, err = dataKey.Seal(p.sanitizer.HTML(msg.HTML))
		if err != nil {
			return nil, nil, fmt.Errorf("parse: encrypting html body: %w", err)
		}
	}
	var textBody []byte
	if len(msg.Text) > 0 {
		textBody, err = dataKey.Seal(msg.Text)
		if err != nil {
			return nil, nil, fmt.Errorf("parse: encrypting text body: %w", err)
		}
	}

	stored := &core.ParsedMessage{
		Subject:  msg.Subject,
		FromName: msg.From.Name,
		FromAddr: msg.From.Addr,
		ToAddrs:  msg.To,
		SentAt:   msg.SentAt,
		TextBody: textBody,
		HTMLBody: htmlBody,
		Preview:  msg.Preview(),
	}

	// The search text is stored in the tsvector, which is not encrypted: a
	// searchable index of the words in a message is inherently a partial
	// disclosure of it, and pretending otherwise would be worse than saying so.
	searchText := strings.TrimSpace(string(msg.Text))
	if searchText == "" {
		searchText = stored.Preview
	}

	return &result{message: msg, stored: stored, searchText: searchText}, attachments, nil
}

func (p *Parser) storeAttachment(ctx context.Context, meta mimeparse.AttachmentMeta, body io.Reader, dataKey *crypto.DataKey) (*core.Attachment, error) {
	content, err := io.ReadAll(io.LimitReader(body, p.maxAttachmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("parse: reading attachment: %w", err)
	}

	attachment := &core.Attachment{
		Filename:    meta.Filename,
		ContentType: mimeparse.ContentTypeOf(meta.ContentType),
		SizeBytes:   int64(len(content)),
	}
	if int64(len(content)) > p.maxAttachmentBytes {
		// Record what arrived without storing it, so the message still lists
		// what it carried rather than silently dropping a part.
		attachment.SizeBytes = p.maxAttachmentBytes
		return attachment, nil
	}

	sealed, err := dataKey.Seal(content)
	if err != nil {
		return nil, fmt.Errorf("parse: encrypting attachment: %w", err)
	}
	sha, size, err := p.blobs.Put(ctx, bytes.NewReader(sealed))
	if err != nil {
		return nil, fmt.Errorf("parse: storing attachment: %w", err)
	}

	var blobID core.UUID
	err = p.db.InTx(ctx, func(q pg.Querier) error {
		id, _, err := pg.AcquireBlob(ctx, q, sha, size, p.blobs.Locate(sha))
		blobID = id
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("parse: recording attachment blob: %w", err)
	}
	attachment.BlobID = &blobID
	return attachment, nil
}
