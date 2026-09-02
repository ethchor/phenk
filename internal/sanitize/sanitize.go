// Package sanitize turns hostile HTML from strangers into something safe to
// render.
//
// Two defences, not one. This package strips everything executable and rewrites
// every remote image through a proxy; the client then renders the result inside
// a sandboxed iframe with no script and no same-origin access. Either layer
// alone would be a single point of failure, and email HTML is the most hostile
// input this system handles.
//
// Sanitizing happens once, on the way in, before the body is encrypted and
// stored. No code path can read unsanitized HTML back out of the database,
// which is what makes invariant 8 structural rather than a rule people have to
// remember.
package sanitize

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
)

// ImageProxyPrefix is the path remote images are rewritten to.
const ImageProxyPrefix = "/i/"

// Sanitizer rewrites and sanitizes message HTML. It is safe for concurrent use.
type Sanitizer struct {
	policy *bluemonday.Policy
	key    []byte
}

// New returns a Sanitizer. The key signs image proxy URLs; derive it from the
// master key so the proxy cannot be pointed at arbitrary hosts by anyone who
// can guess a path.
func New(key []byte) *Sanitizer {
	return &Sanitizer{policy: newPolicy(), key: append([]byte(nil), key...)}
}

// newPolicy builds the allowlist. Everything not named here is removed, which
// is the only defensible default for markup written by strangers.
func newPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// Tables carry the layout of essentially every marketing email ever sent.
	p.AllowTables()
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor",
		"cellpadding", "cellspacing", "border").
		OnElements("table", "thead", "tbody", "tfoot", "tr", "td", "th")
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor").
		OnElements("div", "p", "span", "img", "center", "font")
	p.AllowElements("center", "font", "hr", "small", "big", "u", "s")
	p.AllowAttrs("color", "face", "size").OnElements("font")

	// Links open in a new tab and cannot reach back into the opener. The
	// sandboxed iframe makes that doubly true, but a message body is also
	// quoted and forwarded, so the markup itself should be safe standing alone.
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)

	// Images are rewritten to a relative proxy path before this policy runs,
	// so relative URLs have to survive it.
	p.AllowRelativeURLs(true)
	p.RequireParseableURLs(true)
	p.AllowURLSchemes("http", "https", "mailto", "cid")

	// Inline styles are how email does layout, so stripping them entirely
	// would leave every message looking broken. These are the properties that
	// cannot move content out of the iframe, load a remote resource, or
	// overlay the surrounding page.
	p.AllowStyles(
		"color", "background-color", "background",
		"font", "font-family", "font-size", "font-style", "font-weight",
		"text-align", "text-decoration", "text-transform", "letter-spacing",
		"line-height", "vertical-align", "white-space", "direction",
		"margin", "margin-top", "margin-bottom", "margin-left", "margin-right",
		"padding", "padding-top", "padding-bottom", "padding-left", "padding-right",
		"border", "border-top", "border-bottom", "border-left", "border-right",
		"border-color", "border-style", "border-width", "border-radius",
		"border-collapse", "border-spacing",
		"width", "max-width", "min-width", "height", "max-height", "min-height",
		"display", "float", "clear", "list-style-type",
	).Globally()

	return p
}

// HTML rewrites remote images and then sanitizes the markup.
//
// The rewrite runs first so that the policy sees, and validates, the relative
// proxy paths that will actually be rendered rather than the original remote
// URLs.
func (s *Sanitizer) HTML(raw []byte) []byte {
	rewritten, err := s.rewriteImages(raw)
	if err != nil {
		// Unparseable markup still goes through the policy, which tokenises
		// rather than parses and copes with input a tree builder will not.
		rewritten = raw
	}
	return s.policy.SanitizeBytes(rewritten)
}

// ProxyPath returns the signed proxy path for a remote image URL.
//
// The signature is what stops the proxy being a general purpose fetcher for
// anyone who can construct a path. Without it, /i/ would happily retrieve
// whatever a stranger asked for, from inside the network, which is the classic
// way an image proxy becomes an SSRF vector.
func (s *Sanitizer) ProxyPath(remote string) string {
	return ImageProxyPrefix + s.Sign(remote) + "?u=" + url.QueryEscape(remote)
}

// Sign returns the signature for a remote URL.
func (s *Sanitizer) Sign(remote string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(remote))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// Verify reports whether a signature matches a URL, in constant time.
func (s *Sanitizer) Verify(signature, remote string) bool {
	return hmac.Equal([]byte(signature), []byte(s.Sign(remote)))
}

// rewriteImages points every remote image at the proxy. A message body that
// loaded images directly would report the reader's IP address, user agent and
// read time straight back to the sender, which is exactly the tracking a
// throwaway address exists to avoid.
func (s *Sanitizer) rewriteImages(raw []byte) ([]byte, error) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			s.rewriteNode(n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *Sanitizer) rewriteNode(n *html.Node) {
	for i, attr := range n.Attr {
		if attr.Key != "src" {
			continue
		}
		value := strings.TrimSpace(attr.Val)
		if value == "" || !isRemote(value) {
			// cid: parts are inline attachments and data: URIs carry their own
			// bytes; neither reaches the network, and neither is the policy's
			// problem here.
			continue
		}
		n.Attr[i].Val = s.ProxyPath(value)
	}
	// srcset would reintroduce direct loading through a back door.
	n.Attr = removeAttr(n.Attr, "srcset")
}

func isRemote(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "//")
}

func removeAttr(attrs []html.Attribute, name string) []html.Attribute {
	out := attrs[:0]
	for _, a := range attrs {
		if a.Key != name {
			out = append(out, a)
		}
	}
	return out
}
