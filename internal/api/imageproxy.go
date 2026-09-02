package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/sanitize"
)

// Image proxy limits. A proxy is a machine that fetches whatever it is told to,
// so every one of these is load bearing.
const (
	proxyTimeout     = 5 * time.Second
	proxyMaxBytes    = 2 << 20
	proxyMaxRedirect = 3
	// proxyCacheTTL lets a browser and a CDN keep an image, which is the whole
	// reason the path is stable per URL.
	proxyCacheTTL = 24 * time.Hour
)

// errBlockedAddress is returned when a URL resolves somewhere the proxy will
// not go.
var errBlockedAddress = errors.New("api: address is not routable from the proxy")

// imageProxy fetches remote images on a reader's behalf.
//
// Message HTML never loads images directly: doing so reports the reader's
// address, user agent and read time straight back to whoever sent the message,
// which is exactly the tracking a throwaway address exists to avoid.
//
// Fetching them here instead means this service will connect to a URL a
// stranger chose. Everything below exists because of that: the signature check
// so only URLs we rewrote are fetched, and the address checks so a signed URL
// still cannot reach anything inside the network.
type imageProxy struct {
	sanitizer *sanitize.Sanitizer
	client    *http.Client
}

func newImageProxy(sanitizer *sanitize.Sanitizer) *imageProxy {
	return newImageProxyAllowing(sanitizer)
}

// newImageProxyAllowing builds a proxy that additionally permits a fixed set of
// hosts.
//
// Only tests use the allowance, and only to reach an httptest server, which
// necessarily listens on loopback. Weakening isPublicAddress to accommodate
// that would have meant testing something other than the code that runs in
// production.
func newImageProxyAllowing(sanitizer *sanitize.Sanitizer, allowed ...string) *imageProxy {
	permitted := make(map[string]bool, len(allowed))
	for _, host := range allowed {
		permitted[host] = true
	}
	dialer := &net.Dialer{Timeout: proxyTimeout}

	transport := &http.Transport{
		// Control returns after resolution and immediately before connecting,
		// so a hostname that resolves to a private address is refused at the
		// only moment where the address is both known and not yet dialled.
		// Checking the hostname earlier would lose to DNS rebinding.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", errBlockedAddress, host)
			}
			if !isPublicAddress(addr) && !permitted[host] {
				return nil, fmt.Errorf("%w: %s", errBlockedAddress, addr)
			}
			return dialer.DialContext(ctx, network, address)
		},
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   proxyTimeout,
		ResponseHeaderTimeout: proxyTimeout,
	}

	return &imageProxy{
		sanitizer: sanitizer,
		client: &http.Client{
			Transport: transport,
			Timeout:   proxyTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= proxyMaxRedirect {
					return fmt.Errorf("api: too many redirects")
				}
				// A redirect is a fresh URL the sender also chose, so it gets
				// the same scheme check. Its address is checked by the dialler
				// when the redirect is followed.
				return checkScheme(req.URL)
			},
		},
	}
}

// ProxyImage implements apigen.ServerInterface.
func (s *Server) ProxyImage(w http.ResponseWriter, r *http.Request, signature string, params apigen.ProxyImageParams) {
	remote := params.U

	// Only URLs this service signed while sanitizing a message are fetched.
	// Without this the endpoint would fetch anything anyone asked it to.
	if !s.sanitizer.Verify(signature, remote) {
		writeError(w, http.StatusBadRequest, codeBadRequest, "That image link is not valid")
		return
	}

	target, err := url.Parse(remote)
	if err != nil || checkScheme(target) != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "That image link is not valid")
		return
	}

	body, contentType, err := s.images.fetch(r.Context(), target)
	switch {
	case errors.Is(err, errBlockedAddress):
		writeError(w, http.StatusForbidden, codeNotPermitted, "That image cannot be fetched")
		return
	case err != nil:
		writeError(w, http.StatusBadGateway, codeUnavailable, "That image could not be fetched")
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(proxyCacheTTL.Seconds())))
	// The bytes came from a stranger. Nothing may sniff them into something
	// executable, and nothing may frame or script them.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(body)
}

// fetch retrieves a remote image, or fails.
func (p *imageProxy) fetch(ctx context.Context, target *url.URL) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, proxyTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", err
	}
	// A neutral, fixed set of headers: forwarding the reader's would hand the
	// sender exactly what proxying is meant to withhold.
	request.Header.Set("User-Agent", "phenk-image-proxy/1")
	request.Header.Set("Accept", "image/*")

	response, err := p.client.Do(request)
	if err != nil {
		if errors.Is(err, errBlockedAddress) {
			return nil, "", errBlockedAddress
		}
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("api: image proxy got status %d", response.StatusCode)
	}

	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		// Serving a non-image would turn the proxy into an open relay for
		// arbitrary content on our own origin.
		return nil, "", fmt.Errorf("api: image proxy got content type %q", contentType)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, proxyMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > proxyMaxBytes {
		return nil, "", fmt.Errorf("api: image exceeds %d bytes", proxyMaxBytes)
	}
	return body, contentType, nil
}

// checkScheme refuses anything that is not ordinary web traffic. file:, gopher:
// and the rest are how a fetcher gets turned into a local file reader.
func checkScheme(target *url.URL) error {
	switch strings.ToLower(target.Scheme) {
	case "http", "https":
		if target.Host == "" {
			return fmt.Errorf("api: image url has no host")
		}
		return nil
	default:
		return fmt.Errorf("api: refusing scheme %q", target.Scheme)
	}
}

// isPublicAddress reports whether an address is one the proxy may connect to.
//
// The list is deny-by-default in spirit: everything that is not ordinary public
// internet is refused, because the interesting targets inside a network — a
// metadata service on 169.254.169.254, a database on 10.x, an admin panel on
// localhost — are all reachable by exactly the addresses excluded here.
func isPublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsUnspecified(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsMulticast():
		return false
	}

	if addr.Is4() {
		b := addr.As4()
		switch {
		case b[0] == 0: // "this network"
			return false
		case b[0] == 100 && b[1] >= 64 && b[1] <= 127: // carrier grade NAT
			return false
		case b[0] == 192 && b[1] == 0 && b[2] == 0: // IETF protocol assignments
			return false
		case b[0] == 192 && b[1] == 0 && b[2] == 2: // TEST-NET-1
			return false
		case b[0] == 198 && (b[1] == 18 || b[1] == 19): // benchmarking
			return false
		case b[0] == 198 && b[1] == 51 && b[2] == 100: // TEST-NET-2
			return false
		case b[0] == 203 && b[1] == 0 && b[2] == 113: // TEST-NET-3
			return false
		case b[0] >= 240: // reserved, including broadcast
			return false
		}
		return true
	}

	// IPv6. Unique local addresses are the v6 equivalent of 10.x, and an
	// IPv4-mapped or 6to4 address is a way to smuggle a v4 target past a v6
	// check, so both are refused.
	if addr.Is6() {
		b := addr.As16()
		switch {
		case b[0]&0xfe == 0xfc: // fc00::/7 unique local
			return false
		case b[0] == 0x20 && b[1] == 0x02: // 2002::/16 6to4
			return false
		case b[0] == 0x00 && b[1] == 0x64 && b[2] == 0xff && b[3] == 0x9b: // 64:ff9b::/96 NAT64
			return false
		case b[0] == 0x20 && b[1] == 0x01 && b[2] == 0x00: // 2001:0::/24 teredo and friends
			return false
		}
	}
	return true
}
