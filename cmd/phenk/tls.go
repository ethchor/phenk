package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ethchor/phenk/internal/config"
)

// certReloader serves the SMTP listener's certificate, re-reading it from disk
// when it changes.
//
// The certificate is issued by something outside this process and renewed on
// its own schedule — roughly every sixty days for Let's Encrypt. Reading the
// keypair once at startup would mean serving an expired certificate from the
// day it renewed until somebody noticed and restarted the process, which is
// the kind of outage that arrives two months after the deployment anybody was
// watching.
type certReloader struct {
	certFile string
	keyFile  string

	mu       sync.RWMutex
	cert     *tls.Certificate
	certMod  time.Time
	keyMod   time.Time
	lastWarn time.Time
}

// load reads the keypair if either file has changed since the last read.
//
// A missing file is reported as-is rather than treated as an error worth
// stopping for: the caller decides whether a certificate that is not there yet
// is fatal.
func (c *certReloader) load() (*tls.Certificate, error) {
	certInfo, err := os.Stat(c.certFile)
	if err != nil {
		return nil, err
	}
	keyInfo, err := os.Stat(c.keyFile)
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	cached, certMod, keyMod := c.cert, c.certMod, c.keyMod
	c.mu.RUnlock()

	if cached != nil && certInfo.ModTime().Equal(certMod) && keyInfo.ModTime().Equal(keyMod) {
		return cached, nil
	}

	cert, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cert, c.certMod, c.keyMod = &cert, certInfo.ModTime(), keyInfo.ModTime()
	c.mu.Unlock()

	if cached != nil {
		slog.Info("smtp tls certificate reloaded", "cert_file", c.certFile)
	}
	return &cert, nil
}

// getCertificate is the tls.Config hook. It falls back to the last certificate
// that loaded cleanly, so a renewal that briefly writes a truncated file does
// not take STARTTLS down with it.
func (c *certReloader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert, err := c.load()
	if err == nil {
		return cert, nil
	}

	c.mu.RLock()
	cached := c.cert
	c.mu.RUnlock()
	if cached != nil {
		c.warn("smtp tls certificate could not be re-read, serving the previous one", err)
		return cached, nil
	}
	return nil, fmt.Errorf("loading smtp tls certificate: %w", err)
}

// warn logs at most once a minute. A handshake-path log line is otherwise a
// way to turn a certificate problem into a disk-space problem.
func (c *certReloader) warn(msg string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.lastWarn) < time.Minute {
		return
	}
	c.lastWarn = time.Now()
	slog.Warn(msg, "cert_file", c.certFile, "error", err)
}

// loadTLS builds the configuration the SMTP listener offers for STARTTLS.
// Config validation has already established that the two file paths are set
// together or not at all.
//
// A configured certificate that does not exist yet returns a nil config rather
// than an error. On a first deployment the certificate is issued by a separate
// process that may not have finished, and refusing to start would mean
// refusing mail; advertising STARTTLS and then failing the handshake would be
// worse still, since a sender that took us up on it generally gives up rather
// than retrying in the clear. Serving cleartext until the certificate exists
// loses nothing that was not already going to be lost.
func loadTLS(cfg *config.Config) (*tls.Config, error) {
	if cfg.SMTP.TLSCertFile == "" {
		return nil, nil
	}

	reloader := &certReloader{certFile: cfg.SMTP.TLSCertFile, keyFile: cfg.SMTP.TLSKeyFile}
	if _, err := reloader.load(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			slog.Warn("smtp tls certificate not present, starting without starttls",
				"cert_file", cfg.SMTP.TLSCertFile,
				"hint", "restart once the certificate has been issued")
			return nil, nil
		}
		return nil, fmt.Errorf("loading smtp tls certificate: %w", err)
	}

	return &tls.Config{
		GetCertificate: reloader.getCertificate,
		MinVersion:     tls.VersionTLS12,
	}, nil
}
