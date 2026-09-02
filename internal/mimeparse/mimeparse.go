// Package mimeparse turns a raw MIME message into structured output.
//
// Everything it produces is derived and rebuildable: the raw blob is the
// record, and parsed output can be dropped and regenerated at will. That is
// what makes it safe for this package to be permissive. A message that only
// half parses still yields whatever was legible, because a user who cannot read
// their verification code is not helped by a strictly correct refusal.
package mimeparse

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // registers the non-UTF-8 charsets real mail uses
	gomail "github.com/emersion/go-message/mail"
)

// Defaults for Options.
const (
	// DefaultMaxBodyBytes caps how much decoded body text is retained. A
	// message is capped at 25MB on the wire, but transfer encodings and
	// charset conversion can expand that, and a body is held in memory while
	// it is encrypted.
	DefaultMaxBodyBytes = 2 << 20

	// DefaultMaxParts bounds how many MIME parts are walked, so a message
	// nested a million levels deep cannot occupy a worker indefinitely.
	DefaultMaxParts = 200

	// PreviewLength is how much of the body the message list shows.
	PreviewLength = 200
)

// Address is a parsed mail address.
type Address struct {
	Name string
	Addr string
}

// String renders the address for display.
func (a Address) String() string {
	if a.Name == "" {
		return a.Addr
	}
	return a.Name + " <" + a.Addr + ">"
}

// AttachmentMeta describes one attachment. The body is handed to the Options
// callback rather than held here, so a large attachment streams to storage
// instead of sitting in memory.
type AttachmentMeta struct {
	Filename    string
	ContentType string
	// ContentID is set for inline parts referenced by a cid: URL in the HTML.
	ContentID string
	Inline    bool
}

// Message is the structured form of a raw MIME message.
type Message struct {
	Subject string
	From    Address
	To      []string
	Cc      []string
	ReplyTo []string
	SentAt  *time.Time

	// Text and HTML are the decoded bodies. HTML is raw here: sanitizing is a
	// separate step, and this package never returns markup it has pretended to
	// make safe.
	Text []byte
	HTML []byte

	Attachments []AttachmentMeta

	// Warnings records what could not be understood. A message with warnings
	// still parsed; a message that could not be parsed at all returns an
	// error instead.
	Warnings []string
}

// Preview returns the short summary the message list shows.
func (m *Message) Preview() string {
	source := string(m.Text)
	if strings.TrimSpace(source) == "" {
		source = textFromHTML(m.HTML)
	}
	return truncate(collapseSpace(source), PreviewLength)
}

// Options configures parsing.
type Options struct {
	MaxBodyBytes int64
	MaxParts     int

	// Attachment receives each attachment body as it is reached. Returning an
	// error aborts the parse. A nil callback discards bodies but still records
	// their metadata, which is what the pure parser tests want.
	Attachment func(meta AttachmentMeta, body io.Reader) error
}

func (o *Options) withDefaults() {
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if o.MaxParts <= 0 {
		o.MaxParts = DefaultMaxParts
	}
}

// Parse reads a raw MIME message.
func Parse(r io.Reader, opts Options) (*Message, error) {
	opts.withDefaults()

	// go-message treats an empty stream as an empty message rather than an
	// error. A zero byte DATA is not a message, and calling it one would put a
	// blank row in front of a user with no way to tell why.
	buffered := bufio.NewReader(r)
	if _, err := buffered.Peek(1); err != nil {
		return nil, errors.New("mimeparse: message is empty")
	}

	entity, err := message.Read(buffered)
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("mimeparse: reading message: %w", err)
	}
	if entity == nil {
		return nil, errors.New("mimeparse: message has no content")
	}

	out := &Message{}
	if err != nil {
		out.Warnings = append(out.Warnings, err.Error())
	}

	reader := gomail.NewReader(entity)
	out.readHeader(reader.Header)

	parts := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if message.IsUnknownCharset(err) || message.IsUnknownEncoding(err) {
				// The part is still readable, just not decodable. Record it
				// and carry on rather than losing the rest of the message.
				out.Warnings = append(out.Warnings, err.Error())
				continue
			}
			out.Warnings = append(out.Warnings, "unreadable part: "+err.Error())
			break
		}

		parts++
		if parts > opts.MaxParts {
			out.Warnings = append(out.Warnings, "message has more parts than will be read")
			break
		}

		if err := out.readPart(part, &opts); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func (m *Message) readHeader(h gomail.Header) {
	if subject, err := h.Subject(); err == nil {
		m.Subject = sanitizeHeaderText(subject)
	} else {
		m.Subject = sanitizeHeaderText(h.Get("Subject"))
		if m.Subject != "" {
			m.Warnings = append(m.Warnings, "subject could not be decoded cleanly")
		}
	}

	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		m.From = Address{Name: sanitizeHeaderText(from[0].Name), Addr: strings.ToLower(from[0].Address)}
	} else if raw := h.Get("From"); raw != "" {
		// A From header too broken for the address parser is still worth
		// showing: the reader can usually tell who sent it.
		m.From = Address{Name: sanitizeHeaderText(raw)}
		m.Warnings = append(m.Warnings, "from header could not be parsed as an address")
	}

	m.To = addressList(h, "To")
	m.Cc = addressList(h, "Cc")
	m.ReplyTo = addressList(h, "Reply-To")

	if date, err := h.Date(); err == nil && !date.IsZero() {
		utc := date.UTC()
		m.SentAt = &utc
	} else if raw := h.Get("Date"); raw != "" {
		if parsed, err := mail.ParseDate(raw); err == nil {
			utc := parsed.UTC()
			m.SentAt = &utc
		} else {
			m.Warnings = append(m.Warnings, "date header could not be parsed")
		}
	}
}

func addressList(h gomail.Header, field string) []string {
	list, err := h.AddressList(field)
	if err != nil || len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

func (m *Message) readPart(part *gomail.Part, opts *Options) error {
	switch header := part.Header.(type) {
	case *gomail.InlineHeader:
		contentType, _, _ := header.ContentType()
		body, err := io.ReadAll(io.LimitReader(part.Body, opts.MaxBodyBytes))
		if err != nil {
			m.Warnings = append(m.Warnings, "body part could not be read: "+err.Error())
			return nil
		}
		switch {
		case strings.EqualFold(contentType, "text/html"):
			// Later HTML parts append rather than replace: a multipart/related
			// body can legitimately arrive in pieces.
			m.HTML = append(m.HTML, body...)
		case strings.HasPrefix(strings.ToLower(contentType), "text/"):
			m.Text = append(m.Text, body...)
		default:
			// An inline part that is not text is an attachment in all but
			// name, and inline images are exactly this.
			return m.readAttachment(AttachmentMeta{
				ContentType: contentType,
				ContentID:   strings.Trim(header.Get("Content-Id"), "<>"),
				Inline:      true,
			}, part.Body, opts)
		}

	case *gomail.AttachmentHeader:
		filename, err := header.Filename()
		if err != nil {
			m.Warnings = append(m.Warnings, "attachment filename could not be decoded")
		}
		contentType, _, _ := header.ContentType()
		return m.readAttachment(AttachmentMeta{
			Filename:    safeFilename(filename),
			ContentType: contentType,
			ContentID:   strings.Trim(header.Get("Content-Id"), "<>"),
		}, part.Body, opts)
	}
	return nil
}

func (m *Message) readAttachment(meta AttachmentMeta, body io.Reader, opts *Options) error {
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	m.Attachments = append(m.Attachments, meta)

	if opts.Attachment == nil {
		_, _ = io.Copy(io.Discard, body)
		return nil
	}
	if err := opts.Attachment(meta, body); err != nil {
		return fmt.Errorf("mimeparse: storing attachment %q: %w", meta.Filename, err)
	}
	return nil
}

// safeFilename strips anything that would let an attachment name escape the
// directory it is offered from, or misrepresent its own extension.
func safeFilename(name string) string {
	name = sanitizeHeaderText(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(strings.Trim(name, "."))
	if name == "" {
		return ""
	}
	if len(name) > 255 {
		name = name[:255]
	}
	return name
}

// sanitizeHeaderText removes control characters and repairs invalid UTF-8, so
// a header can be stored in a text column and rendered without surprises. A
// header is attacker controlled, and CR or LF inside one is how header
// injection works.
func sanitizeHeaderText(s string) string {
	if s == "" {
		return ""
	}
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary so the preview never ends mid-character.
	trimmed := s[:n]
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return strings.TrimSpace(trimmed) + "…"
}

// textFromHTML is a crude tag stripper, used only to build a preview when a
// message has no plain text part. It is never used to render anything.
func textFromHTML(h []byte) string {
	var b strings.Builder
	depth := 0
	for _, r := range string(h) {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return unescapeEntities(b.String())
}

// unescapeEntities resolves the handful of entities that show up in previews.
func unescapeEntities(s string) string {
	return strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
	).Replace(s)
}

// ContentTypeOf normalises a media type for storage.
func ContentTypeOf(raw string) string {
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "application/octet-stream"
	}
	return strings.ToLower(mediaType)
}
