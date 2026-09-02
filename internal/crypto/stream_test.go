package crypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
)

func newTestContentKey(t *testing.T) *DataKey {
	t.Helper()
	key, _, err := NewContentKey()
	if err != nil {
		t.Fatalf("NewContentKey: %v", err)
	}
	return key
}

func roundTrip(t *testing.T, key *DataKey, plaintext []byte) []byte {
	t.Helper()
	var sealed bytes.Buffer
	n, err := key.SealStream(&sealed, bytes.NewReader(plaintext))
	if err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	if n != int64(len(plaintext)) {
		t.Fatalf("SealStream consumed %d bytes, want %d", n, len(plaintext))
	}

	var opened bytes.Buffer
	m, err := key.OpenStream(&opened, bytes.NewReader(sealed.Bytes()))
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if m != int64(len(plaintext)) {
		t.Fatalf("OpenStream produced %d bytes, want %d", m, len(plaintext))
	}
	return opened.Bytes()
}

func TestStreamRoundTripAcrossSizes(t *testing.T) {
	key := newTestContentKey(t)

	sizes := []int{
		0, 1, 100,
		streamChunkSize - 1, streamChunkSize, streamChunkSize + 1,
		3*streamChunkSize + 17,
	}
	for _, size := range sizes {
		plaintext := make([]byte, size)
		if _, err := rand.Read(plaintext); err != nil {
			t.Fatalf("rand: %v", err)
		}
		got := roundTrip(t, key, plaintext)
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("round trip of %d bytes changed the content", size)
		}
	}
}

func TestStreamCiphertextDoesNotLeakPlaintext(t *testing.T) {
	key := newTestContentKey(t)
	plaintext := []byte(strings.Repeat("Subject: your code is 481920\r\n", 5000))

	var sealed bytes.Buffer
	if _, err := key.SealStream(&sealed, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	if bytes.Contains(sealed.Bytes(), []byte("481920")) {
		t.Fatal("the encrypted stream contains the plaintext")
	}
	if sealed.Len() <= len(plaintext) {
		t.Fatal("the encrypted stream is not larger than the plaintext, so it carries no authentication")
	}
}

func TestStreamIsNonDeterministic(t *testing.T) {
	key := newTestContentKey(t)
	plaintext := []byte("the same message twice")

	var a, b bytes.Buffer
	if _, err := key.SealStream(&a, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	if _, err := key.SealStream(&b, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	if bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("sealing the same plaintext twice produced identical streams")
	}
}

func TestTruncatedStreamIsRejected(t *testing.T) {
	// A truncated message that decrypted cleanly would be worse than one that
	// fails: the reader would see a message that silently lost its ending.
	key := newTestContentKey(t)
	plaintext := make([]byte, 3*streamChunkSize)
	if _, err := rand.Read(plaintext); err != nil {
		t.Fatalf("rand: %v", err)
	}

	var sealed bytes.Buffer
	if _, err := key.SealStream(&sealed, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	full := sealed.Bytes()

	for _, cut := range []int{len(full) - 1, len(full) / 2, len(full) / 3, 5} {
		var out bytes.Buffer
		if _, err := key.OpenStream(&out, bytes.NewReader(full[:cut])); err == nil {
			t.Fatalf("a stream truncated to %d of %d bytes decrypted cleanly", cut, len(full))
		}
	}
}

func TestTamperedStreamIsRejected(t *testing.T) {
	key := newTestContentKey(t)
	plaintext := []byte(strings.Repeat("x", 3*streamChunkSize))

	var sealed bytes.Buffer
	if _, err := key.SealStream(&sealed, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("SealStream: %v", err)
	}

	for _, at := range []int{6, len(sealed.Bytes()) / 2, sealed.Len() - 1} {
		tampered := bytes.Clone(sealed.Bytes())
		tampered[at] ^= 0xff
		var out bytes.Buffer
		if _, err := key.OpenStream(&out, bytes.NewReader(tampered)); err == nil {
			t.Fatalf("a stream tampered with at byte %d decrypted cleanly", at)
		}
	}
}

func TestReorderedFramesAreRejected(t *testing.T) {
	// Frames are bound to their position by the nonce counter, so swapping two
	// of them must not produce a message with its paragraphs rearranged.
	key := newTestContentKey(t)
	plaintext := make([]byte, 2*streamChunkSize)
	for i := range plaintext {
		plaintext[i] = byte(i / streamChunkSize)
	}

	var sealed bytes.Buffer
	if _, err := key.SealStream(&sealed, bytes.NewReader(plaintext)); err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	raw := sealed.Bytes()

	// Frames start after the 5 byte header; each is a 4 byte length plus body.
	const headerLen = 1 + streamPrefixLen
	frameLen := 4 + streamChunkSize + 16
	if len(raw) < headerLen+2*frameLen {
		t.Skip("stream layout changed")
	}
	swapped := bytes.Clone(raw)
	copy(swapped[headerLen:headerLen+frameLen], raw[headerLen+frameLen:headerLen+2*frameLen])
	copy(swapped[headerLen+frameLen:headerLen+2*frameLen], raw[headerLen:headerLen+frameLen])

	var out bytes.Buffer
	if _, err := key.OpenStream(&out, bytes.NewReader(swapped)); err == nil {
		t.Fatal("reordered frames decrypted cleanly")
	}
}

func TestStreamFromAnotherKeyIsRejected(t *testing.T) {
	mine := newTestContentKey(t)
	theirs := newTestContentKey(t)

	var sealed bytes.Buffer
	if _, err := mine.SealStream(&sealed, strings.NewReader("private")); err != nil {
		t.Fatalf("SealStream: %v", err)
	}
	var out bytes.Buffer
	if _, err := theirs.OpenStream(&out, bytes.NewReader(sealed.Bytes())); err == nil {
		t.Fatal("a stream decrypted under the wrong key")
	}
}

func TestOpenStreamRejectsGarbage(t *testing.T) {
	key := newTestContentKey(t)
	for _, input := range [][]byte{nil, {1}, {9, 0, 0, 0, 0, 0, 0, 0, 1}, bytes.Repeat([]byte{0xff}, 64)} {
		var out bytes.Buffer
		if _, err := key.OpenStream(&out, bytes.NewReader(input)); err == nil {
			t.Fatalf("garbage input %v decrypted cleanly", input)
		}
	}

	var out bytes.Buffer
	_, err := key.OpenStream(&out, bytes.NewReader([]byte{99, 1, 2, 3, 4}))
	if !errors.Is(err, ErrStreamFormat) {
		t.Fatalf("got %v, want ErrStreamFormat for an unknown version", err)
	}
}

func TestStreamRefusesADestroyedKey(t *testing.T) {
	key := newTestContentKey(t)
	var sealed bytes.Buffer
	if _, err := key.SealStream(&sealed, strings.NewReader("secret")); err != nil {
		t.Fatalf("SealStream: %v", err)
	}

	key.Destroy()
	if _, err := key.SealStream(io.Discard, strings.NewReader("more")); !errors.Is(err, ErrKeyDestroyed) {
		t.Fatalf("SealStream after Destroy = %v, want ErrKeyDestroyed", err)
	}
	if _, err := key.OpenStream(io.Discard, bytes.NewReader(sealed.Bytes())); !errors.Is(err, ErrKeyDestroyed) {
		t.Fatalf("OpenStream after Destroy = %v, want ErrKeyDestroyed", err)
	}
}

func TestContentKeyWrappingRoundTrip(t *testing.T) {
	// The envelope: a content key encrypts the blob, and each identity that
	// received the message stores that key wrapped under its own data key.
	kr := testKeyring(t)
	identityKey, _, err := kr.NewDataKey(newTestUUID())
	if err != nil {
		t.Fatalf("NewDataKey: %v", err)
	}

	contentKey, raw, err := NewContentKey()
	if err != nil {
		t.Fatalf("NewContentKey: %v", err)
	}
	wrapped, err := identityKey.Seal(raw)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	var sealed bytes.Buffer
	if _, err := contentKey.SealStream(&sealed, strings.NewReader("the raw message")); err != nil {
		t.Fatalf("SealStream: %v", err)
	}

	// Recovering it goes the other way: unwrap with the identity key, rebuild
	// the content key, decrypt the blob.
	recoveredRaw, err := identityKey.Open(wrapped)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	recovered, err := ContentKeyFrom(recoveredRaw)
	if err != nil {
		t.Fatalf("ContentKeyFrom: %v", err)
	}
	var out bytes.Buffer
	if _, err := recovered.OpenStream(&out, bytes.NewReader(sealed.Bytes())); err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if out.String() != "the raw message" {
		t.Fatalf("recovered %q", out.String())
	}

	// Destroying the identity key is what makes the blob unreadable: the
	// wrapping can no longer be opened, however many bytes survive on disk.
	identityKey.Destroy()
	if _, err := identityKey.Open(wrapped); !errors.Is(err, ErrKeyDestroyed) {
		t.Fatalf("got %v, want ErrKeyDestroyed", err)
	}
}
