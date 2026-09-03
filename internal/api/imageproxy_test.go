package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestSSRFProbesAreRefused(t *testing.T) {
	// The acceptance criterion for this phase, and the reason the proxy exists
	// in this shape at all. Every one of these is a real place an image proxy
	// has been used to reach inside a network.
	h := newHarness(t)
	c := h.anonymous()

	targets := []string{
		"http://169.254.169.254/latest/meta-data/",            // AWS instance metadata
		"http://metadata.google.internal/computeMetadata/v1/", // GCP metadata
		"http://127.0.0.1:8080/admin",                         // loopback
		"http://localhost/admin",                              // loopback by name
		"http://[::1]/admin",                                  // loopback, v6
		"http://10.0.0.1/",                                    // private
		"http://172.16.0.1/",                                  // private
		"http://192.168.1.1/",                                 // private
		"http://[fd00::1]/",                                   // unique local, v6
		"http://0.0.0.0/",                                     // unspecified
		"http://100.64.0.1/",                                  // carrier grade NAT
	}

	for _, target := range targets {
		// The URL is correctly signed: this is not testing the signature, it is
		// testing that a signature is not enough.
		path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
		response := c.do(http.MethodGet, path, nil)
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			t.Errorf("%s was fetched, want a refusal", target)
		}
		if response.StatusCode >= 500 && response.StatusCode != http.StatusBadGateway {
			t.Errorf("%s returned %d", target, response.StatusCode)
		}
	}
}

func TestUnsignedImageURLsAreRefused(t *testing.T) {
	// Without the signature the proxy would fetch whatever anyone asked it to,
	// which is how an image proxy becomes a general purpose fetcher.
	h := newHarness(t)
	c := h.anonymous()

	target := "https://example.test/logo.png"
	for _, path := range []string{
		"/i/deadbeef?u=" + url.QueryEscape(target),
		"/i/" + h.server.sanitizer.Sign("https://other.test/logo.png") + "?u=" + url.QueryEscape(target),
	} {
		response := c.do(http.MethodGet, path, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", path, response.StatusCode)
		}
	}
}

func TestNonHTTPSchemesAreRefused(t *testing.T) {
	h := newHarness(t)
	c := h.anonymous()

	for _, target := range []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:11211/",
		"ftp://internal.test/",
		"data:text/html,<script>alert(1)</script>",
	} {
		path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
		response := c.do(http.MethodGet, path, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", target, response.StatusCode)
		}
	}
}

func TestIsPublicAddress(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "0.0.0.0", "10.1.2.3", "172.16.0.1", "172.31.255.255",
		"192.168.0.1", "169.254.169.254", "100.64.0.1", "224.0.0.1",
		"240.0.0.1", "255.255.255.255", "192.0.2.1", "198.18.0.1", "203.0.113.1",
		"::1", "::", "fd00::1", "fe80::1", "ff02::1", "2002::1", "64:ff9b::1",
	}
	for _, s := range blocked {
		addr := netip.MustParseAddr(s)
		if isPublicAddress(addr) {
			t.Errorf("%s was treated as public", s)
		}
	}

	public := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111"}
	for _, s := range public {
		addr := netip.MustParseAddr(s)
		if !isPublicAddress(addr) {
			t.Errorf("%s was treated as private", s)
		}
	}

	// An IPv4-mapped v6 address must be judged as the v4 address it carries,
	// or it becomes a way to smuggle a private target past the check.
	if isPublicAddress(netip.MustParseAddr("::ffff:127.0.0.1")) {
		t.Error("an ipv4-mapped loopback address was treated as public")
	}
	if isPublicAddress(netip.MustParseAddr("::ffff:10.0.0.1")) {
		t.Error("an ipv4-mapped private address was treated as public")
	}
}

func TestProxyServesARealImage(t *testing.T) {
	// The proxy has to actually work, or every message would render with
	// broken images and the tracking protection would be moot.
	h := newHarness(t)
	c := h.anonymous()

	const pixel = "\x89PNG\r\n\x1a\n fake image bytes"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(pixel))
	}))
	defer origin.Close()

	// The test origin is on loopback, which the proxy refuses by design, so it
	// is allowed through explicitly for this one test rather than by weakening
	// the rule everywhere.
	h.server.images = newImageProxyAllowing(h.server.sanitizer, originHost(t, origin.URL))

	target := origin.URL + "/logo.png"
	path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
	response := c.do(http.MethodGet, path, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("content type = %q", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("x-content-type-options = %q", got)
	}
	if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("content security policy = %q", got)
	}
}

func TestProxyRefusesNonImages(t *testing.T) {
	// Serving arbitrary content from our own origin is exactly what an open
	// relay is.
	h := newHarness(t)
	c := h.anonymous()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer origin.Close()
	h.server.images = newImageProxyAllowing(h.server.sanitizer, originHost(t, origin.URL))

	target := origin.URL + "/not-an-image"
	path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
	response := c.do(http.MethodGet, path, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", response.StatusCode)
	}
}

func TestProxyRefusesOversizedImages(t *testing.T) {
	h := newHarness(t)
	c := h.anonymous()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(make([]byte, proxyMaxBytes+1024))
	}))
	defer origin.Close()
	h.server.images = newImageProxyAllowing(h.server.sanitizer, originHost(t, origin.URL))

	target := origin.URL + "/huge.png"
	path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
	response := c.do(http.MethodGet, path, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", response.StatusCode)
	}
}

func TestProxyRefusesARedirectToAPrivateAddress(t *testing.T) {
	// A redirect is a second URL the sender also chose, so it gets the same
	// treatment as the first.
	h := newHarness(t)
	c := h.anonymous()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer origin.Close()
	h.server.images = newImageProxyAllowing(h.server.sanitizer, originHost(t, origin.URL))

	target := origin.URL + "/redirect.png"
	path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
	response := c.do(http.MethodGet, path, nil)
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("a redirect reached the metadata service")
	}
}

// originHost returns the IP an httptest server listens on. The proxy checks
// addresses after resolution, so the allowance is keyed by address rather than
// by name.
func originHost(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing test origin: %v", err)
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return parsed.Host
	}
	return host
}

func TestProxyFetchesAnImageByHostname(t *testing.T) {
	// The regression this exists for: an earlier version checked the address
	// in a DialContext override, which is handed the hostname rather than the
	// resolved address. Every literal IP behaved correctly and every real
	// image — which is to say every image — was refused as unparseable.
	//
	// "localhost" resolves to loopback, which the test allowance permits, so
	// this exercises a named host end to end without weakening the rule.
	h := newHarness(t)
	c := h.anonymous()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer origin.Close()
	h.server.images = newImageProxyAllowing(h.server.sanitizer, "127.0.0.1", "::1")

	_, port, err := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))
	if err != nil {
		t.Fatalf("splitting the test origin: %v", err)
	}
	target := "http://localhost:" + port + "/logo.png"

	path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
	response := c.do(http.MethodGet, path, nil)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: a hostname that resolves to an allowed address was refused", response.StatusCode)
	}
}

func TestProxyRefusesAHostnameThatResolvesPrivately(t *testing.T) {
	// The check runs after resolution, so a name pointing into the network is
	// refused even though the name itself says nothing.
	h := newHarness(t)
	c := h.anonymous()
	// No allowance here: loopback is private, and localhost resolves to it.
	target := "http://localhost:9/secret"

	path := "/i/" + h.server.sanitizer.Sign(target) + "?u=" + url.QueryEscape(target)
	response := c.do(http.MethodGet, path, nil)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", response.StatusCode)
	}
}
