package smtpd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

// selfSignedTLS builds a certificate good enough to negotiate STARTTLS in a
// test. Real deployments supply their own; what matters here is that the
// upgrade happens and is recorded on the delivery.
func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mx.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"mx.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}}}
}

func TestStartTLSIsOfferedAndRecorded(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TLSConfig = selfSignedTLS(t) })
	identity, address := h.allocate("session-1")

	// The listener must advertise STARTTLS before anyone can use it.
	plain, err := smtp.Dial(h.addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if err := plain.Hello("sender.test"); err != nil {
		t.Fatalf("EHLO: %v", err)
	}
	if ok, _ := plain.Extension("STARTTLS"); !ok {
		t.Fatal("STARTTLS was not offered")
	}
	_ = plain.Quit()
	_ = plain.Close()

	client, err := smtp.DialStartTLS(h.addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("DialStartTLS: %v", err)
	}
	defer client.Close()
	if err := client.Hello("sender.test"); err != nil {
		t.Fatalf("EHLO after upgrade: %v", err)
	}

	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt(address, nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}
	w, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	if _, err := w.Write([]byte(message("encrypted"))); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = client.Quit()

	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != 1 {
		t.Fatalf("%d deliveries, want 1", len(deliveries))
	}
	if !deliveries[0].TLS {
		t.Fatal("the delivery does not record that it arrived over TLS")
	}
}

func TestCleartextIsStillAcceptedWhenTLSIsConfigured(t *testing.T) {
	// STARTTLS is opportunistic. An MX that refused cleartext would refuse a
	// large slice of real mail, so a sender that does not upgrade must still
	// be able to deliver — and the delivery must say so.
	h := newHarness(t, func(c *Config) { c.TLSConfig = selfSignedTLS(t) })
	identity, address := h.allocate("session-1")

	if err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("cleartext")); err != nil {
		t.Fatalf("send: %v", err)
	}
	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != 1 {
		t.Fatalf("%d deliveries, want 1", len(deliveries))
	}
	if deliveries[0].TLS {
		t.Fatal("a cleartext delivery was recorded as encrypted")
	}
}
