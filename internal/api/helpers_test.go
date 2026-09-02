package api

import (
	"net/netip"

	"github.com/ethchor/phenk/internal/sanitize"
	"github.com/ethchor/phenk/internal/worker/parse"
)

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

func (h *harness) parser() *parse.Parser {
	return parse.New(h.db, h.blobs, h.keyring,
		sanitize.New(h.keyring.Derive("image-proxy")), parse.Options{})
}

// testMessage is a small but realistic message.
func testMessage(subject, body string) string {
	return "From: Sender <sender@example.com>\r\n" +
		"To: recipient@phenk.test\r\n" +
		"Subject: " + subject + "\r\n" +
		"Date: Mon, 02 Jan 2006 15:04:05 -0700\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body + "\r\n"
}

// htmlMessage carries a tracking pixel and a script, so the sanitizing path is
// exercised end to end.
func htmlMessage(subject string) string {
	return "From: Sender <sender@example.com>\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		`<html><body><script>alert(1)</script>` +
		`<img src="https://tracker.test/pixel.gif?id=victim">` +
		`<p>Your code is 481920.</p></body></html>` + "\r\n"
}

var _ = sanitize.ImageProxyPrefix
