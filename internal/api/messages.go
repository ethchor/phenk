package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/worker/parse"
)

// defaultPageSize is used when a caller names no limit.
const defaultPageSize = 50

// ListMessages implements apigen.ServerInterface.
func (s *Server) ListMessages(w http.ResponseWriter, r *http.Request, id apigen.IdentityId, params apigen.ListMessagesParams) {
	identity, _, ok := s.ownedIdentity(w, r, id)
	if !ok {
		return
	}
	s.writeMessageList(w, r, identity, since(params.Since), limit(params.Limit))
}

// writeMessageList renders one page of an inbox.
func (s *Server) writeMessageList(w http.ResponseWriter, r *http.Request, identity *core.Identity, sinceSeq int64, pageSize int) {
	summaries, cursor, err := s.messagePage(r, identity, sinceSeq, pageSize)
	if err != nil {
		internalError(w, r, "listing messages", err)
		return
	}
	writeJSON(w, http.StatusOK, apigen.MessageList{Messages: summaries, Cursor: cursor})
}

// messagePage loads and renders the deliveries after a cursor.
func (s *Server) messagePage(r *http.Request, identity *core.Identity, sinceSeq int64, pageSize int) ([]apigen.MessageSummary, int64, error) {
	deliveries, err := pg.DeliveriesSince(r.Context(), s.db, identity.ID, sinceSeq, pageSize)
	if err != nil {
		return nil, 0, err
	}

	// An empty page leaves the caller's cursor where it was, so a poll that
	// finds nothing does not accidentally skip past a message committed a
	// moment later.
	cursor := sinceSeq
	summaries := make([]apigen.MessageSummary, 0, len(deliveries))
	for i := range deliveries {
		delivery := &deliveries[i]
		summary, err := s.summarize(r, identity, delivery)
		if err != nil {
			return nil, 0, err
		}
		summaries = append(summaries, summary)
		cursor = delivery.Seq
	}
	return summaries, cursor, nil
}

// summarize renders a delivery for a list. A message that has not been parsed
// yet still appears, with what is known of it: the alternative is an inbox that
// looks empty for a second after mail has visibly arrived.
func (s *Server) summarize(r *http.Request, identity *core.Identity, delivery *core.Delivery) (apigen.MessageSummary, error) {
	summary := apigen.MessageSummary{
		Id:         mustAPIUUID(delivery.ID),
		Seq:        delivery.Seq,
		ReceivedAt: delivery.ReceivedAt,
		SizeBytes:  delivery.SizeBytes,
		State:      apigen.MessageSummaryState(delivery.State),
		From:       apigen.Address{Address: delivery.EnvelopeFrom},
		Auth: apigen.AuthResults{
			Spf:   authResult(delivery.SPF),
			Dkim:  authResult(delivery.DKIM),
			Dmarc: authResult(delivery.DMARC),
		},
	}

	parsed, err := pg.ParsedMessageByDelivery(r.Context(), s.db, delivery.ID)
	if errors.Is(err, pg.ErrNotFound) {
		return summary, nil
	}
	if err != nil {
		return summary, err
	}

	summary.Subject = parsed.Subject
	summary.Preview = parsed.Preview
	summary.From = apigen.Address{Address: parsed.FromAddr}
	if parsed.FromName != "" {
		name := parsed.FromName
		summary.From.Name = &name
	}
	if parsed.FromAddr == "" {
		summary.From.Address = delivery.EnvelopeFrom
	}
	summary.SentAt = parsed.SentAt

	attachments, err := pg.AttachmentsForDelivery(r.Context(), s.db, delivery.ID)
	if err != nil {
		return summary, err
	}
	summary.AttachmentCount = len(attachments)
	return summary, nil
}

// GetMessage implements apigen.ServerInterface.
func (s *Server) GetMessage(w http.ResponseWriter, r *http.Request, id apigen.MessageId) {
	delivery, identity, ok := s.readableMessage(w, r, id)
	if !ok {
		return
	}

	summary, err := s.summarize(r, identity, delivery)
	if err != nil {
		internalError(w, r, "reading a message", err)
		return
	}
	message := messageFromSummary(summary)

	parsed, err := pg.ParsedMessageByDelivery(r.Context(), s.db, delivery.ID)
	switch {
	case errors.Is(err, pg.ErrNotFound):
		// Not parsed yet, or the parse failed. The raw message is still
		// downloadable, which is the point of keeping it.
		writeJSON(w, http.StatusOK, message)
		return
	case err != nil:
		internalError(w, r, "reading a message", err)
		return
	}

	dataKey, err := s.keyring.Unwrap(identity.ID, identity.WrappedDataKey)
	if err != nil {
		internalError(w, r, "unwrapping a data key", err)
		return
	}
	defer dataKey.Destroy()

	if len(parsed.TextBody) > 0 {
		text, err := dataKey.Open(parsed.TextBody)
		if err != nil {
			internalError(w, r, "decrypting a text body", err)
			return
		}
		body := string(text)
		message.Text = &body
	}
	if len(parsed.HTMLBody) > 0 {
		html, err := dataKey.Open(parsed.HTMLBody)
		if err != nil {
			internalError(w, r, "decrypting an html body", err)
			return
		}
		// This was sanitized before it was ever stored. It is still only safe
		// inside a sandboxed iframe with no scripting, and the schema says so.
		body := string(html)
		message.Html = &body
	}
	if parsed.ToAddrs != nil {
		message.To = parsed.ToAddrs
	}

	attachments, err := pg.AttachmentsForDelivery(r.Context(), s.db, delivery.ID)
	if err != nil {
		internalError(w, r, "listing attachments", err)
		return
	}
	for _, a := range attachments {
		message.Attachments = append(message.Attachments, apigen.Attachment{
			Id:          mustAPIUUID(a.ID),
			Filename:    a.Filename,
			ContentType: a.ContentType,
			SizeBytes:   a.SizeBytes,
			Available:   a.BlobID != nil,
		})
	}
	writeJSON(w, http.StatusOK, message)
}

// GetRawMessage implements apigen.ServerInterface.
func (s *Server) GetRawMessage(w http.ResponseWriter, r *http.Request, id apigen.MessageId) {
	delivery, identity, ok := s.readableMessage(w, r, id)
	if !ok {
		return
	}

	raw, err := s.decryptBlob(r, identity, delivery.BlobID, delivery.WrappedContentKey)
	if err != nil {
		internalError(w, r, "reading a raw message", err)
		return
	}

	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.Header().Set("Cache-Control", "no-store")
	// A raw message is bytes from a stranger. Telling the browser to download
	// rather than display it keeps it out of the page entirely.
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", delivery.ID.String()+".eml"))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}

// GetAttachment implements apigen.ServerInterface.
func (s *Server) GetAttachment(w http.ResponseWriter, r *http.Request, id apigen.MessageId, attachmentID openapiUUID) {
	delivery, identity, ok := s.readableMessage(w, r, id)
	if !ok {
		return
	}

	attachments, err := pg.AttachmentsForDelivery(r.Context(), s.db, delivery.ID)
	if err != nil {
		internalError(w, r, "listing attachments", err)
		return
	}
	wanted := coreUUID(attachmentID)
	for _, a := range attachments {
		if a.ID != wanted {
			continue
		}
		if a.BlobID == nil {
			notFound(w)
			return
		}
		content, err := s.decryptAttachment(r, identity, *a.BlobID)
		if err != nil {
			internalError(w, r, "reading an attachment", err)
			return
		}
		// Attachments are always downloaded, never rendered: an HTML or SVG
		// attachment displayed inline would run in this origin.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName(a.Filename, a.ID.String())))
		_, _ = w.Write(content)
		return
	}
	notFound(w)
}

// readableMessage resolves a message and checks the caller may read it.
//
// A message on a random identity needs the owner session. A message on a named
// identity needs nothing, because a public inbox has no owner — which is
// exactly why named identities can never hold a grant.
func (s *Server) readableMessage(w http.ResponseWriter, r *http.Request, id apigen.MessageId) (*core.Delivery, *core.Identity, bool) {
	deliveryID := coreUUID(id)

	delivery, err := pg.DeliveryByID(r.Context(), s.db, deliveryID)
	if errors.Is(err, pg.ErrNotFound) {
		notFound(w)
		return nil, nil, false
	}
	if err != nil {
		internalError(w, r, "loading a message", err)
		return nil, nil, false
	}

	identity, err := pg.IdentityByID(r.Context(), s.db, delivery.IdentityID)
	if err != nil {
		internalError(w, r, "loading an identity", err)
		return nil, nil, false
	}

	switch identity.Kind {
	case core.KindNamed:
		// Public by construction.
	case core.KindRandom:
		session := s.session(r)
		if session == "" || identity.OwnerSession != session {
			notFound(w)
			return nil, nil, false
		}
	default:
		notFound(w)
		return nil, nil, false
	}

	if len(identity.WrappedDataKey) == 0 {
		// Purged. There is nothing left to read, and saying so any more
		// precisely would confirm the address once existed.
		notFound(w)
		return nil, nil, false
	}
	return delivery, identity, true
}

// decryptBlob reads a raw message through the envelope: identity data key,
// then the delivery's wrapping of the content key, then the blob.
func (s *Server) decryptBlob(r *http.Request, identity *core.Identity, blobID core.UUID, wrappedContentKey []byte) ([]byte, error) {
	dataKey, err := s.keyring.Unwrap(identity.ID, identity.WrappedDataKey)
	if err != nil {
		return nil, err
	}
	defer dataKey.Destroy()

	contentKey, err := parse.UnwrapContentKey(dataKey, wrappedContentKey)
	if err != nil {
		return nil, err
	}
	defer contentKey.Destroy()

	rc, sha, err := s.openBlob(r, blobID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var out bytes.Buffer
	if _, err := contentKey.OpenStream(&out, rc); err != nil {
		return nil, fmt.Errorf("api: decrypting blob %s: %w", sha, err)
	}
	return out.Bytes(), nil
}

// decryptAttachment reads an attachment, which is encrypted under the identity
// data key directly rather than through a content key: an attachment blob
// belongs to exactly one delivery and is never shared.
func (s *Server) decryptAttachment(r *http.Request, identity *core.Identity, blobID core.UUID) ([]byte, error) {
	dataKey, err := s.keyring.Unwrap(identity.ID, identity.WrappedDataKey)
	if err != nil {
		return nil, err
	}
	defer dataKey.Destroy()

	rc, _, err := s.openBlob(r, blobID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	sealed, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return dataKey.Open(sealed)
}

func (s *Server) openBlob(r *http.Request, blobID core.UUID) (io.ReadCloser, blob.SHA256, error) {
	var sha blob.SHA256
	row, err := pg.BlobByID(r.Context(), s.db, blobID)
	if err != nil {
		return nil, sha, err
	}
	sha, err = blob.SHA256FromBytes(row.SHA256)
	if err != nil {
		return nil, sha, err
	}
	rc, err := s.blobs.Get(r.Context(), sha)
	return rc, sha, err
}

func authResult(result core.AuthResult) apigen.AuthResult {
	if result == "" {
		return apigen.AuthResult(core.AuthNone)
	}
	return apigen.AuthResult(result)
}

func since(v *int64) int64 {
	if v == nil || *v < 0 {
		return 0
	}
	return *v
}

func limit(v *int) int {
	if v == nil || *v <= 0 {
		return defaultPageSize
	}
	if *v > 200 {
		return 200
	}
	return *v
}

// downloadName keeps an attachment's name usable without letting it carry
// quotes or newlines into the Content-Disposition header.
func downloadName(filename, fallback string) string {
	cleaned := make([]rune, 0, len(filename))
	for _, r := range filename {
		if r < 0x20 || r == '"' || r == '\\' || r == '/' {
			continue
		}
		cleaned = append(cleaned, r)
	}
	if len(cleaned) == 0 {
		return fallback
	}
	return string(cleaned)
}

// messageFromSummary widens a list entry into a full message. The generated
// types flatten the specification's allOf, so the shared fields are copied
// rather than embedded.
func messageFromSummary(summary apigen.MessageSummary) apigen.Message {
	return apigen.Message{
		Id:              summary.Id,
		Seq:             summary.Seq,
		Subject:         summary.Subject,
		Preview:         summary.Preview,
		From:            summary.From,
		ReceivedAt:      summary.ReceivedAt,
		SentAt:          summary.SentAt,
		SizeBytes:       summary.SizeBytes,
		State:           apigen.MessageState(summary.State),
		AttachmentCount: summary.AttachmentCount,
		Auth:            summary.Auth,
		To:              []string{},
		Attachments:     []apigen.Attachment{},
	}
}

// crypto is referenced for its errors in this file's callers.
var _ = crypto.ErrKeyDestroyed
