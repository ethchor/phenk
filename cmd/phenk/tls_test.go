package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/config"
)

// writeKeyPair writes a self-signed certificate for name, and returns nothing:
// the test cares about which certificate was served, not what is in it.
func writeKeyPair(t *testing.T, certFile, keyFile, name string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		t.Fatalf("writing certificate: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("writing key: %v", err)
	}
}

// commonName reads the subject back out of a served certificate, which is how
// these tests tell one generated certificate from another.
func commonName(t *testing.T, cert *tls.Certificate) string {
	t.Helper()
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing served certificate: %v", err)
	}
	return parsed.Subject.CommonName
}

func TestLoadTLSWithoutConfiguredFilesIsNotAnError(t *testing.T) {
	cfg := &config.Config{}
	tlsConfig, err := loadTLS(cfg)
	if err != nil {
		t.Fatalf("loadTLS: %v", err)
	}
	if tlsConfig != nil {
		t.Fatal("expected no tls config when no certificate is configured")
	}
}

// A first deployment starts before the certificate exists. Refusing to start
// would refuse mail; advertising STARTTLS without a certificate would lose the
// senders that took us up on it.
func TestLoadTLSWithAMissingCertificateStartsWithoutStartTLS(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.SMTP.TLSCertFile = filepath.Join(dir, "cert.pem")
	cfg.SMTP.TLSKeyFile = filepath.Join(dir, "key.pem")

	tlsConfig, err := loadTLS(cfg)
	if err != nil {
		t.Fatalf("loadTLS: %v", err)
	}
	if tlsConfig != nil {
		t.Fatal("expected no tls config when the certificate has not been issued yet")
	}
}

// A malformed certificate is a misconfiguration, not a race with issuance.
func TestLoadTLSWithAMalformedCertificateFails(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, []byte("not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.SMTP.TLSCertFile = certFile
	cfg.SMTP.TLSKeyFile = keyFile

	if _, err := loadTLS(cfg); err == nil {
		t.Fatal("expected a malformed certificate to fail")
	}
}

// The renewal case. Sixty days after deployment the files change underneath a
// running process, and it has to notice.
func TestCertificateIsReloadedAfterRenewal(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	writeKeyPair(t, certFile, keyFile, "before.example")

	cfg := &config.Config{}
	cfg.SMTP.TLSCertFile = certFile
	cfg.SMTP.TLSKeyFile = keyFile

	tlsConfig, err := loadTLS(cfg)
	if err != nil {
		t.Fatalf("loadTLS: %v", err)
	}
	if tlsConfig == nil {
		t.Fatal("expected a tls config")
	}

	served, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if got := commonName(t, served); got != "before.example" {
		t.Fatalf("served %q, want before.example", got)
	}

	// Renewal. The modification time has to move for the reloader to see it,
	// and a test can outrun the filesystem's timestamp resolution.
	writeKeyPair(t, certFile, keyFile, "after.example")
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(keyFile, future, future); err != nil {
		t.Fatal(err)
	}

	served, err = tlsConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate after renewal: %v", err)
	}
	if got := commonName(t, served); got != "after.example" {
		t.Fatalf("served %q after renewal, want after.example", got)
	}
}

// A renewal that is briefly half-written must not take STARTTLS down. The
// previous certificate is still valid; serving it beats failing the handshake.
func TestATruncatedRenewalKeepsServingThePreviousCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	writeKeyPair(t, certFile, keyFile, "before.example")

	cfg := &config.Config{}
	cfg.SMTP.TLSCertFile = certFile
	cfg.SMTP.TLSKeyFile = keyFile

	tlsConfig, err := loadTLS(cfg)
	if err != nil {
		t.Fatalf("loadTLS: %v", err)
	}

	if err := os.WriteFile(certFile, []byte("-----BEGIN CERTIFICATE-----\ntrunc"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatal(err)
	}

	served, err := tlsConfig.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate during a partial write: %v", err)
	}
	if got := commonName(t, served); got != "before.example" {
		t.Fatalf("served %q, want the previous certificate before.example", got)
	}
}
