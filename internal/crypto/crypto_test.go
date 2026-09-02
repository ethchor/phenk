package crypto

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ethchor/phenk/internal/core"
)

func testKeyring(t *testing.T) *Keyring {
	t.Helper()
	master, err := ParseMasterKey(GenerateMasterKey())
	if err != nil {
		t.Fatalf("ParseMasterKey: %v", err)
	}
	kr, err := NewKeyring(master)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func TestNewKeyringRejectsWrongSize(t *testing.T) {
	if _, err := NewKeyring(make([]byte, 16)); !errors.Is(err, ErrKeySize) {
		t.Fatalf("got %v, want ErrKeySize", err)
	}
}

func TestParseMasterKey(t *testing.T) {
	if _, err := ParseMasterKey(GenerateMasterKey()); err != nil {
		t.Fatalf("standard encoding: %v", err)
	}
	if _, err := ParseMasterKey("not base64!!"); err == nil {
		t.Fatal("expected failure on non-base64 input")
	}
	if _, err := ParseMasterKey("c2hvcnQ="); !errors.Is(err, ErrKeySize) {
		t.Fatalf("got %v, want ErrKeySize for a short key", err)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	kr := testKeyring(t)
	id := core.NewUUID()
	dk, wrapped, err := kr.NewDataKey(id)
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}

	plaintext := []byte("Subject: hello\r\n\r\nreal mail")
	ct, err := dk.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatal("ciphertext contains the plaintext")
	}

	// A fresh unwrap of the stored key must read what the original sealed:
	// this is the path a worker takes when it picks up a delivery later.
	reloaded, err := kr.Unwrap(id, wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	got, err := reloaded.Open(ct)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Open = %q, want %q", got, plaintext)
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	kr := testKeyring(t)
	dk, _, err := kr.NewDataKey(core.NewUUID())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	a, _ := dk.Seal([]byte("same"))
	b, _ := dk.Seal([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("sealing the same plaintext twice produced identical ciphertext")
	}
}

func TestWrappedKeyIsBoundToItsIdentity(t *testing.T) {
	kr := testKeyring(t)
	victim := core.NewUUID()
	attacker := core.NewUUID()
	_, wrapped, err := kr.NewDataKey(victim)
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	// Lifting one identity's wrapped key onto another identity row must not
	// yield a usable key.
	if _, err := kr.Unwrap(attacker, wrapped); err == nil {
		t.Fatal("unwrapped a data key under the wrong identity id")
	}
}

func TestUnwrapRejectsTamperedAndMalformedInput(t *testing.T) {
	kr := testKeyring(t)
	id := core.NewUUID()
	_, wrapped, err := kr.NewDataKey(id)
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}

	tampered := bytes.Clone(wrapped)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := kr.Unwrap(id, tampered); err == nil {
		t.Fatal("unwrapped a tampered key")
	}

	if _, err := kr.Unwrap(id, []byte{1, 2, 3}); !errors.Is(err, ErrWrapFormat) {
		t.Fatalf("got %v, want ErrWrapFormat", err)
	}

	badVersion := bytes.Clone(wrapped)
	badVersion[0] = 99
	if _, err := kr.Unwrap(id, badVersion); !errors.Is(err, ErrWrapFormat) {
		t.Fatalf("got %v, want ErrWrapFormat", err)
	}
}

func TestOpenRejectsTamperedAndTruncatedCiphertext(t *testing.T) {
	kr := testKeyring(t)
	dk, _, err := kr.NewDataKey(core.NewUUID())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	ct, err := dk.Seal([]byte("hello"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	ct[len(ct)-1] ^= 0xff
	if _, err := dk.Open(ct); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("got %v, want ErrCiphertext", err)
	}
	if _, err := dk.Open([]byte{1, 2}); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("got %v, want ErrCiphertext for a truncated value", err)
	}
}

func TestDestroyRendersTheKeyUnusable(t *testing.T) {
	kr := testKeyring(t)
	dk, _, err := kr.NewDataKey(core.NewUUID())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	ct, err := dk.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	dk.Destroy()
	if !dk.Destroyed() {
		t.Fatal("Destroyed() = false after Destroy()")
	}
	if _, err := dk.Open(ct); !errors.Is(err, ErrKeyDestroyed) {
		t.Fatalf("Open after Destroy = %v, want ErrKeyDestroyed", err)
	}
	if _, err := dk.Seal([]byte("more")); !errors.Is(err, ErrKeyDestroyed) {
		t.Fatalf("Seal after Destroy = %v, want ErrKeyDestroyed", err)
	}
	dk.Destroy() // idempotent
}

func TestEmptyPlaintextRoundTrips(t *testing.T) {
	kr := testKeyring(t)
	dk, _, err := kr.NewDataKey(core.NewUUID())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}
	ct, err := dk.Seal(nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := dk.Open(ct)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Open = %q, want empty", got)
	}
}
