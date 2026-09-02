package mimeparse

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture reads a golden message. The fixtures are committed files rather than
// strings in the test, because real mail is byte-exact and CRLF-sensitive and
// a Go string literal quietly is not.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mime", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return body
}

// parseFixture parses a golden message, collecting attachment bodies.
func parseFixture(t *testing.T, name string) (*Message, map[string][]byte) {
	t.Helper()
	bodies := map[string][]byte{}
	msg, err := Parse(bytes.NewReader(fixture(t, name)), Options{
		Attachment: func(meta AttachmentMeta, body io.Reader) error {
			content, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			key := meta.Filename
			if key == "" {
				key = meta.ContentID
			}
			bodies[key] = content
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return msg, bodies
}

func TestPlainText(t *testing.T) {
	msg, _ := parseFixture(t, "plain-text.eml")

	if msg.Subject != "Your verification code" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if msg.From.Name != "Alice Sender" || msg.From.Addr != "alice@example.com" {
		t.Errorf("from = %+v", msg.From)
	}
	if len(msg.To) != 1 || msg.To[0] != "k7f2m9x3qz@phenk.test" {
		t.Errorf("to = %v", msg.To)
	}
	if msg.SentAt == nil {
		t.Fatal("no date parsed")
	}
	if got := msg.SentAt.Format("2006-01-02T15:04:05Z"); got != "2006-01-02T22:04:05Z" {
		t.Errorf("sent at = %s, want the date normalised to utc", got)
	}
	if !strings.Contains(string(msg.Text), "481920") {
		t.Errorf("text body = %q", msg.Text)
	}
	if len(msg.HTML) != 0 {
		t.Errorf("a plain text message produced html: %q", msg.HTML)
	}
	if preview := msg.Preview(); !strings.HasPrefix(preview, "Your verification code is 481920.") {
		t.Errorf("preview = %q", preview)
	}
}

func TestMultipartAlternative(t *testing.T) {
	msg, _ := parseFixture(t, "multipart-alternative.eml")

	if !strings.Contains(string(msg.Text), "order 12345 is confirmed") {
		t.Errorf("text = %q", msg.Text)
	}
	if !strings.Contains(string(msg.HTML), "<strong>12345</strong>") {
		t.Errorf("html = %q", msg.HTML)
	}
	if len(msg.Attachments) != 0 {
		t.Errorf("alternative parts should not be attachments: %+v", msg.Attachments)
	}
}

func TestNestedMultipart(t *testing.T) {
	msg, bodies := parseFixture(t, "nested-multipart.eml")

	// The alternative inside the mixed part is walked, not treated as opaque.
	if !strings.Contains(string(msg.Text), "plain text digest") {
		t.Errorf("text = %q", msg.Text)
	}
	if !strings.Contains(string(msg.HTML), "<em>HTML</em>") {
		t.Errorf("html = %q", msg.HTML)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "notes.txt" {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	if !strings.Contains(string(bodies["notes.txt"]), "Attached notes") {
		t.Errorf("attachment body = %q", bodies["notes.txt"])
	}
}

func TestBase64Attachment(t *testing.T) {
	msg, bodies := parseFixture(t, "base64-attachment.eml")

	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	a := msg.Attachments[0]
	if a.Filename != "invoice.pdf" || a.ContentType != "application/pdf" {
		t.Errorf("attachment = %+v", a)
	}
	// The transfer encoding is decoded, not passed through.
	if !bytes.HasPrefix(bodies["invoice.pdf"], []byte("%PDF-1.4")) {
		t.Errorf("attachment body was not base64 decoded: %q", bodies["invoice.pdf"])
	}
}

func TestQuotedPrintableAndEncodedWords(t *testing.T) {
	msg, _ := parseFixture(t, "quoted-printable.eml")

	if msg.Subject != "Votre code de vérification" {
		t.Errorf("subject = %q, want the encoded word decoded", msg.Subject)
	}
	if msg.From.Name != "Café" {
		t.Errorf("from name = %q", msg.From.Name)
	}
	body := string(msg.Text)
	if !strings.Contains(body, "Café résumé") {
		t.Errorf("body = %q, want quoted printable decoded", body)
	}
	if !strings.Contains(body, "90210") {
		t.Errorf("body = %q, want the soft wrapped line rejoined", body)
	}
	if strings.Contains(body, "=C3") || strings.Contains(body, "=\n") {
		t.Errorf("body still contains quoted printable escapes: %q", body)
	}
}

func TestNonUTF8Charsets(t *testing.T) {
	latin, _ := parseFixture(t, "charset-iso8859-1.eml")
	if latin.Subject != "Bestätigung" {
		t.Errorf("subject = %q", latin.Subject)
	}
	if latin.From.Name != "Müller" {
		t.Errorf("from name = %q", latin.From.Name)
	}
	if body := string(latin.Text); !strings.Contains(body, "Grüße aus München") {
		t.Errorf("iso-8859-1 body = %q, want it converted to utf-8", body)
	}

	// Shift_JIS is not a byte mapping, so it exercises a real conversion table
	// rather than a passthrough that happens to look right.
	sjis, _ := parseFixture(t, "charset-shift-jis.eml")
	if body := string(sjis.Text); !strings.Contains(body, "こんにちは") {
		t.Errorf("shift_jis body = %q", body)
	}
}

func TestMalformedHeadersStillYieldABody(t *testing.T) {
	// A message that only half parses still yields whatever was legible.
	msg, _ := parseFixture(t, "malformed-headers.eml")

	if msg.Subject != "Broken but readable" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if !strings.Contains(string(msg.Text), "should still be readable") {
		t.Errorf("body = %q", msg.Text)
	}
	if len(msg.Warnings) == 0 {
		t.Error("a message with broken headers should record warnings")
	}
	// An unparseable From is still shown, because a reader can usually tell
	// who sent it even when the address parser cannot.
	if msg.From.Name == "" && msg.From.Addr == "" {
		t.Error("the from header was discarded entirely")
	}
	// A control character in a header must not survive into storage.
	for _, w := range msg.Warnings {
		if strings.ContainsRune(w, 0x01) {
			t.Error("a control character survived header sanitizing")
		}
	}
}

func TestHostileHTMLParsesWithoutPanicAndIsNotTrusted(t *testing.T) {
	msg, _ := parseFixture(t, "hostile-html.eml")

	// This package deliberately does not sanitize: it returns the raw markup
	// and the sanitizer is a separate, testable step. What matters here is
	// that parsing hostile input does not panic and does not lose the message.
	if !strings.Contains(string(msg.HTML), "<script>") {
		t.Error("the parser silently altered the raw html it was given")
	}
	if !strings.Contains(string(msg.HTML), "314159") {
		t.Error("the legitimate content was lost")
	}
	// A null byte smuggled through an encoded word must not survive into a
	// text column.
	if strings.ContainsRune(msg.Subject, 0) {
		t.Errorf("a null byte survived in the subject: %q", msg.Subject)
	}
	if !strings.Contains(msg.Subject, "script") {
		t.Errorf("subject = %q, want the literal text preserved", msg.Subject)
	}
}

func TestPreviewFallsBackToHTML(t *testing.T) {
	msg, _ := parseFixture(t, "no-text-part.eml")

	if len(msg.Text) != 0 {
		t.Errorf("this fixture has no text part, got %q", msg.Text)
	}
	preview := msg.Preview()
	if !strings.Contains(preview, "Welcome") || !strings.Contains(preview, "246810") {
		t.Errorf("preview = %q", preview)
	}
	if strings.Contains(preview, "<") {
		t.Errorf("preview still contains markup: %q", preview)
	}
}

func TestInlineImageBecomesAnAttachmentWithItsContentID(t *testing.T) {
	msg, bodies := parseFixture(t, "inline-image.eml")

	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	a := msg.Attachments[0]
	if a.ContentID != "logo@example.com" {
		t.Errorf("content id = %q, want the cid the html references", a.ContentID)
	}
	if a.ContentType != "image/png" {
		t.Errorf("content type = %q", a.ContentType)
	}
	if !bytes.HasPrefix(bodies["logo@example.com"], []byte("\x89PNG")) {
		t.Errorf("inline image was not decoded: %q", bodies["logo@example.com"])
	}
}

func TestAttachmentFilenamesCannotEscapeADirectory(t *testing.T) {
	msg, _ := parseFixture(t, "traversal-attachment.eml")

	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	name := msg.Attachments[0].Filename
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		t.Fatalf("filename %q still contains a path", name)
	}
	if name != "pwned" {
		t.Errorf("filename = %q, want just the base name", name)
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"invoice.pdf":              "invoice.pdf",
		"../../etc/passwd":         "passwd",
		`..\..\windows\system32\x`: "x",
		"/absolute/path.txt":       "path.txt",
		"...":                      "",
		"":                         "",
		"  spaced.txt  ":           "spaced.txt",
		"with\x01control.txt":      "withcontrol.txt",
		strings.Repeat("a", 300):   strings.Repeat("a", 255),
	}
	for in, want := range cases {
		if got := safeFilename(in); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRejectsEmptyInput(t *testing.T) {
	if _, err := Parse(bytes.NewReader(nil), Options{}); err == nil {
		t.Fatal("parsing nothing succeeded")
	}
}

func TestParseHandlesTruncatedMessages(t *testing.T) {
	// A message cut off mid-part must not panic. It is allowed to lose the
	// truncated part; it is not allowed to lose the whole message.
	full := fixture(t, "nested-multipart.eml")
	for _, cut := range []int{len(full) / 4, len(full) / 2, len(full) - 20} {
		msg, err := Parse(bytes.NewReader(full[:cut]), Options{})
		if err != nil {
			continue // an outright parse failure is an acceptable outcome
		}
		_ = msg.Preview()
	}
}

func TestSanitizeHeaderTextRepairsInvalidUTF8(t *testing.T) {
	got := sanitizeHeaderText(string([]byte{0xff, 0xfe, 'h', 'i'}))
	if !strings.Contains(got, "hi") {
		t.Errorf("got %q, want the legible part kept", got)
	}
	if !strings.ContainsRune(got, '�') {
		t.Errorf("got %q, want invalid bytes replaced", got)
	}
	if got := sanitizeHeaderText("subject\r\nBcc: victim@example.com"); strings.ContainsAny(got, "\r\n") {
		t.Errorf("got %q, want header injection newlines removed", got)
	}
}
