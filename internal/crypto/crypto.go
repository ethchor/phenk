// Package crypto implements Phenk's key hierarchy: one master key held in the
// process environment, and one data encryption key per identity, wrapped under
// the master key and stored beside the identity row.
//
// Everything an identity holds — raw MIME blobs, parsed bodies, attachments —
// is encrypted under that identity's data key. Purging an identity destroys the
// wrapped key, and with it the only way to read anything the identity ever
// received. That is invariant 4 of the v0 plan, and it is why purge is a
// cryptographic operation rather than a delete.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/ethchor/phenk/internal/core"
)

// KeySize is the size of both the master key and every data key.
const KeySize = 32

// wrapVersion prefixes every wrapped data key. A future master key rotation
// bumps it, so old and new wrappings can coexist without a data migration.
const wrapVersion byte = 1

// Errors returned by this package.
var (
	ErrKeySize      = fmt.Errorf("crypto: key must be %d bytes", KeySize)
	ErrKeyDestroyed = errors.New("crypto: data key has been destroyed")
	ErrWrapFormat   = errors.New("crypto: wrapped key is malformed")
	ErrCiphertext   = errors.New("crypto: ciphertext is malformed")
)

// Keyring holds the master key and wraps and unwraps per-identity data keys.
// It is safe for concurrent use.
type Keyring struct {
	aead cipher.AEAD

	// derivationKey is the master key, kept for Derive. Wrapping uses the
	// AEAD above rather than this, so the two uses never share a construction.
	derivationKey []byte
}

// NewKeyring returns a keyring over a 32-byte master key.
func NewKeyring(masterKey []byte) (*Keyring, error) {
	if len(masterKey) != KeySize {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("crypto: master key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: master key: %w", err)
	}
	return &Keyring{aead: aead, derivationKey: append([]byte(nil), masterKey...)}, nil
}

// ParseMasterKey decodes a base64 master key, as it is supplied in the
// environment. Both standard and URL-safe alphabets are accepted, with or
// without padding, because operators paste these by hand.
func ParseMasterKey(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != KeySize {
				return nil, ErrKeySize
			}
			return b, nil
		}
	}
	return nil, errors.New("crypto: master key is not valid base64")
}

// GenerateMasterKey returns a new random master key, base64 encoded for the
// environment. Used by `phenk genkey`.
func GenerateMasterKey() string {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		panic("crypto: random source unavailable: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(k)
}

// NewDataKey mints a data key for an identity and returns it alongside its
// wrapped form. The caller stores the wrapped form and keeps the live key only
// for as long as it needs it.
func (k *Keyring) NewDataKey(identityID core.UUID) (*DataKey, []byte, error) {
	raw := make([]byte, KeySize)
	if _, err := rand.Read(raw); err != nil {
		return nil, nil, fmt.Errorf("crypto: random source unavailable: %w", err)
	}
	wrapped, err := k.wrap(identityID, raw)
	if err != nil {
		return nil, nil, err
	}
	dk, err := newDataKey(raw)
	if err != nil {
		return nil, nil, err
	}
	return dk, wrapped, nil
}

// Unwrap recovers an identity's data key from its stored wrapped form. The
// identity ID is authenticated as additional data, so a wrapped key lifted from
// one identity row cannot be used to read another identity's mail.
func (k *Keyring) Unwrap(identityID core.UUID, wrapped []byte) (*DataKey, error) {
	ns := k.aead.NonceSize()
	if len(wrapped) < 1+ns+KeySize {
		return nil, ErrWrapFormat
	}
	if wrapped[0] != wrapVersion {
		return nil, fmt.Errorf("%w: unknown version %d", ErrWrapFormat, wrapped[0])
	}
	raw, err := k.aead.Open(nil, wrapped[1:1+ns], wrapped[1+ns:], identityID[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: cannot unwrap data key: %w", err)
	}
	return newDataKey(raw)
}

func (k *Keyring) wrap(identityID core.UUID, raw []byte) ([]byte, error) {
	ns := k.aead.NonceSize()
	out := make([]byte, 1+ns, 1+ns+len(raw)+k.aead.Overhead())
	out[0] = wrapVersion
	if _, err := rand.Read(out[1 : 1+ns]); err != nil {
		return nil, fmt.Errorf("crypto: random source unavailable: %w", err)
	}
	return k.aead.Seal(out, out[1:1+ns], raw, identityID[:]), nil
}

// DataKey encrypts and decrypts one identity's content. It is safe for
// concurrent use until it is destroyed.
type DataKey struct {
	raw  []byte
	aead cipher.AEAD
}

func newDataKey(raw []byte) (*DataKey, error) {
	if len(raw) != KeySize {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &DataKey{raw: raw, aead: aead}, nil
}

// Seal encrypts plaintext. The output is nonce || ciphertext || tag.
func (d *DataKey) Seal(plaintext []byte) ([]byte, error) {
	if d.aead == nil {
		return nil, ErrKeyDestroyed
	}
	ns := d.aead.NonceSize()
	out := make([]byte, ns, ns+len(plaintext)+d.aead.Overhead())
	if _, err := rand.Read(out[:ns]); err != nil {
		return nil, fmt.Errorf("crypto: random source unavailable: %w", err)
	}
	return d.aead.Seal(out, out[:ns], plaintext, nil), nil
}

// Open decrypts a value produced by Seal.
func (d *DataKey) Open(ciphertext []byte) ([]byte, error) {
	if d.aead == nil {
		return nil, ErrKeyDestroyed
	}
	ns := d.aead.NonceSize()
	if len(ciphertext) < ns+d.aead.Overhead() {
		return nil, ErrCiphertext
	}
	pt, err := d.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCiphertext, err)
	}
	return pt, nil
}

// Destroy zeroes the key material in memory and renders the key unusable.
//
// This is the in-process half of purging. The durable half is deleting the
// wrapped key from the identity row, which the store does; until that happens
// the data is still recoverable. Destroy is idempotent.
func (d *DataKey) Destroy() {
	for i := range d.raw {
		d.raw[i] = 0
	}
	d.raw = nil
	d.aead = nil
}

// Destroyed reports whether Destroy has been called.
func (d *DataKey) Destroyed() bool { return d.aead == nil }

// Derive returns a 32-byte subkey for a named purpose, so parts of the system
// that need a secret of their own — the image proxy signer, for one — do not
// need an operator to manage a second one, and cannot be used to attack the
// master key or each other.
func (k *Keyring) Derive(purpose string) []byte {
	// The master key is already a uniformly random 32 bytes, so an HMAC with
	// it as the key is a sound extractor without a full HKDF extract step.
	mac := hmac.New(sha256.New, k.derivationKey)
	mac.Write([]byte("phenk-derive-v1:" + purpose))
	return mac.Sum(nil)
}
