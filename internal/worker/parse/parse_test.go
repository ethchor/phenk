package parse

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/sanitize"
	"github.com/ethchor/phenk/internal/store/pg"
)

func TestParseStoresStructuredOutput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("multipart-alternative.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	delivery, err := pg.DeliveryByID(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("DeliveryByID: %v", err)
	}
	if delivery.State != core.DeliveryParsed || delivery.ParsedAt == nil {
		t.Fatalf("delivery = %+v", delivery)
	}

	parsed, err := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("ParsedMessageByDelivery: %v", err)
	}
	if parsed.Subject != "Order confirmed" {
		t.Errorf("subject = %q", parsed.Subject)
	}
	if parsed.FromAddr != "orders@example.com" || parsed.FromName != "Shop" {
		t.Errorf("from = %q <%q>", parsed.FromName, parsed.FromAddr)
	}
	if len(parsed.ToAddrs) != 1 {
		t.Errorf("to = %v", parsed.ToAddrs)
	}
	if parsed.SentAt == nil {
		t.Error("no sent date stored")
	}
	if !strings.Contains(parsed.Preview, "order 12345 is confirmed") {
		t.Errorf("preview = %q", parsed.Preview)
	}
}

func TestBodiesAreEncryptedAtRest(t *testing.T) {
	// Invariant 4: parsed bodies are encrypted under the identity data key.
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("multipart-alternative.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	parsed, err := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("ParsedMessageByDelivery: %v", err)
	}

	// What is stored must not be readable without the key.
	if strings.Contains(string(parsed.TextBody), "12345") {
		t.Fatal("the text body is stored in the clear")
	}
	if strings.Contains(string(parsed.HTMLBody), "12345") {
		t.Fatal("the html body is stored in the clear")
	}

	// And must be readable with it.
	if text := f.decrypt(parsed.TextBody); !strings.Contains(text, "order 12345 is confirmed") {
		t.Errorf("decrypted text = %q", text)
	}
	if html := f.decrypt(parsed.HTMLBody); !strings.Contains(html, "<strong>12345</strong>") {
		t.Errorf("decrypted html = %q", html)
	}
}

func TestHTMLIsSanitizedBeforeItIsStored(t *testing.T) {
	// Sanitizing on the way in is what makes invariant 8 structural: no code
	// path can read unsanitized HTML back out of the database, because none
	// was ever written.
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("hostile-html.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	parsed, err := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("ParsedMessageByDelivery: %v", err)
	}

	html := f.decrypt(parsed.HTMLBody)
	for _, forbidden := range []string{"<script", "onerror", "javascript:", "<iframe", "<form", "position:fixed"} {
		if strings.Contains(strings.ToLower(html), forbidden) {
			t.Errorf("stored html still contains %q:\n%s", forbidden, html)
		}
	}
	// The tracking pixel goes through the proxy rather than loading directly.
	if strings.Contains(html, "tracker.evil.test") && !strings.Contains(html, sanitize.ImageProxyPrefix) {
		t.Error("a tracking pixel is still loaded directly")
	}
	// The legitimate content survives: sanitizing must not eat the message.
	if !strings.Contains(html, "314159") {
		t.Errorf("the verification code was lost:\n%s", html)
	}
}

func TestAttachmentsAreExtractedEncryptedAndRefcounted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("base64-attachment.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	attachments, err := pg.AttachmentsForDelivery(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("AttachmentsForDelivery: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("%d attachments, want 1", len(attachments))
	}
	a := attachments[0]
	if a.Filename != "invoice.pdf" || a.ContentType != "application/pdf" {
		t.Fatalf("attachment = %+v", a)
	}
	if a.BlobID == nil {
		t.Fatal("the attachment body was not stored")
	}

	row, err := pg.BlobByID(ctx, f.db, *a.BlobID)
	if err != nil {
		t.Fatalf("BlobByID: %v", err)
	}
	if row.Refcount != 1 {
		t.Fatalf("attachment blob refcount = %d, want 1", row.Refcount)
	}

	sha, err := shaOf(row.SHA256)
	if err != nil {
		t.Fatalf("sha: %v", err)
	}
	stored, err := readBlob(ctx, f, sha)
	if err != nil {
		t.Fatalf("reading attachment blob: %v", err)
	}
	if strings.Contains(string(stored), "%PDF") {
		t.Fatal("the attachment is stored in the clear")
	}
	if plain := f.decrypt(stored); !strings.HasPrefix(plain, "%PDF-1.4") {
		t.Fatalf("decrypted attachment = %q", plain[:min(len(plain), 32)])
	}
}

func TestParsingIsIdempotent(t *testing.T) {
	// Parsed output is derived, so re-running the parser must converge rather
	// than accumulate. A retry that raced a success takes this path.
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("nested-multipart.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("first Parse: %v", err)
	}
	firstParsed, _ := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
	firstAttachments, _ := pg.AttachmentsForDelivery(ctx, f.db, deliveryID)

	// A delivery already marked parsed is skipped outright.
	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("second Parse: %v", err)
	}

	// Force a genuine re-parse, as a parser fix applied to old mail would.
	if _, err := f.db.Exec(ctx, `UPDATE deliveries SET state = 'received' WHERE id = $1`, deliveryID); err != nil {
		t.Fatalf("resetting state: %v", err)
	}
	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("re-Parse: %v", err)
	}

	secondParsed, _ := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
	secondAttachments, _ := pg.AttachmentsForDelivery(ctx, f.db, deliveryID)

	if secondParsed.Subject != firstParsed.Subject || secondParsed.Preview != firstParsed.Preview {
		t.Fatal("re-parsing changed the stored message")
	}
	if len(secondAttachments) != len(firstAttachments) {
		t.Fatalf("re-parsing produced %d attachments, want %d: they accumulated",
			len(secondAttachments), len(firstAttachments))
	}

	var rows int
	_ = f.db.QueryRow(ctx, `SELECT count(*) FROM parsed_messages WHERE delivery_id = $1`, deliveryID).Scan(&rows)
	if rows != 1 {
		t.Fatalf("%d parsed_messages rows, want 1", rows)
	}
}

func TestUnparseableMessageIsMarkedFailedAndKeepsItsBlob(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliverRaw(ctx, nil)

	err := f.parser.Parse(ctx, deliveryID)
	if err == nil {
		t.Fatal("parsing an empty message succeeded")
	}
	if !errors.Is(err, ErrPermanent) {
		t.Fatalf("got %v, want a permanent failure: retrying an empty message cannot help", err)
	}

	delivery, err := pg.DeliveryByID(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("DeliveryByID: %v", err)
	}
	if delivery.State != core.DeliveryFailed {
		t.Fatalf("delivery state = %q, want failed", delivery.State)
	}
	// The raw message stays readable whether or not it ever parses.
	if _, err := pg.BlobByID(ctx, f.db, delivery.BlobID); err != nil {
		t.Fatalf("the blob of a failed parse was lost: %v", err)
	}
}

func TestParseSkipsAPurgedIdentity(t *testing.T) {
	// A job that waited while its identity was purged has no key to encrypt
	// under and nothing left worth reading. That is not a failure.
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("plain-text.eml")

	if _, err := pg.SetIdentityState(ctx, f.db, f.identity.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	if _, err := pg.DestroyDataKey(ctx, f.db, f.identity.ID, timeNowPlusYear()); err != nil {
		t.Fatalf("DestroyDataKey: %v", err)
	}

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID); !errors.Is(err, pg.ErrNotFound) {
		t.Fatalf("a purged identity's message was parsed and stored: %v", err)
	}
}

func TestParsedEventIsEmitted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("plain-text.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	events, err := pg.EventsForIdentity(ctx, f.db, f.identity.ID, 0, 100)
	if err != nil {
		t.Fatalf("EventsForIdentity: %v", err)
	}
	var parsed int
	for _, e := range events {
		if e.Type == core.EventMessageParsed {
			parsed++
			if !strings.Contains(string(e.Payload), "Your verification code") {
				t.Errorf("the parsed event carries no subject: %s", e.Payload)
			}
		}
	}
	if parsed != 1 {
		t.Fatalf("%d message.parsed events, want 1", parsed)
	}
}

func TestSearchFindsAParsedMessage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	deliveryID := f.deliver("plain-text.eml")
	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	hits, err := pg.SearchDeliveries(ctx, f.db, f.identity.ID, "verification", 10)
	if err != nil {
		t.Fatalf("SearchDeliveries: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != deliveryID {
		t.Fatalf("search returned %d hits", len(hits))
	}

	none, err := pg.SearchDeliveries(ctx, f.db, f.identity.ID, "unrelatedterm", 10)
	if err != nil {
		t.Fatalf("SearchDeliveries: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("search for an absent term returned %d hits", len(none))
	}
}

func TestEveryGoldenFixtureParses(t *testing.T) {
	// The acceptance criterion for this phase: each fixture parses without
	// panic and produces expected structured output.
	fixtures := []struct {
		name        string
		wantSubject string
		wantText    string
		attachments int
	}{
		{"plain-text.eml", "Your verification code", "481920", 0},
		{"multipart-alternative.eml", "Order confirmed", "12345", 0},
		{"nested-multipart.eml", "Weekly digest", "plain text digest", 1},
		{"base64-attachment.eml", "Your invoice", "Invoice attached", 1},
		{"quoted-printable.eml", "Votre code de vérification", "90210", 0},
		{"charset-iso8859-1.eml", "Bestätigung", "München", 0},
		{"charset-shift-jis.eml", "test", "こんにちは", 0},
		{"malformed-headers.eml", "Broken but readable", "still be readable", 0},
		{"hostile-html.eml", "", "", 0},
		{"no-text-part.eml", "HTML only", "", 0},
		{"inline-image.eml", "With a logo", "", 1},
		{"traversal-attachment.eml", "Attachment with a hostile name", "See attached", 1},
	}

	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			deliveryID := f.deliver(tc.name)

			if err := f.parser.Parse(ctx, deliveryID); err != nil {
				t.Fatalf("Parse: %v", err)
			}
			parsed, err := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
			if err != nil {
				t.Fatalf("ParsedMessageByDelivery: %v", err)
			}
			if tc.wantSubject != "" && parsed.Subject != tc.wantSubject {
				t.Errorf("subject = %q, want %q", parsed.Subject, tc.wantSubject)
			}
			if tc.wantText != "" && !strings.Contains(f.decrypt(parsed.TextBody), tc.wantText) {
				t.Errorf("text body does not contain %q", tc.wantText)
			}
			attachments, err := pg.AttachmentsForDelivery(ctx, f.db, deliveryID)
			if err != nil {
				t.Fatalf("AttachmentsForDelivery: %v", err)
			}
			if len(attachments) != tc.attachments {
				t.Errorf("%d attachments, want %d", len(attachments), tc.attachments)
			}
		})
	}
}

func TestOversizedAttachmentIsListedButNotStored(t *testing.T) {
	f := newFixture(t, func(o *Options) { o.MaxAttachmentBytes = 8 })
	ctx := context.Background()
	deliveryID := f.deliver("base64-attachment.eml")

	if err := f.parser.Parse(ctx, deliveryID); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	attachments, err := pg.AttachmentsForDelivery(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("AttachmentsForDelivery: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("%d attachments, want 1", len(attachments))
	}
	// The message still lists what it carried rather than silently dropping it.
	if attachments[0].Filename != "invoice.pdf" {
		t.Errorf("attachment = %+v", attachments[0])
	}
	if attachments[0].BlobID != nil {
		t.Error("an oversized attachment body was stored anyway")
	}
}
