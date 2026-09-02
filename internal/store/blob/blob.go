// Package blob stores immutable, content-addressed byte streams: raw MIME
// messages and extracted attachments.
//
// Blobs are keyed by the SHA-256 of their contents, so writing the same bytes
// twice is idempotent and one message delivered to two identities is stored
// once. Nothing in this package mutates a blob after it is written: parsed
// output is derived and always rebuildable from the blob, never the other way
// round. That is invariant 3 of the v0 plan.
package blob

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
)

// ErrNotFound is returned by Get and reported by Delete implementations that
// distinguish a missing blob. Delete itself is idempotent and does not return
// it.
var ErrNotFound = errors.New("blob: not found")

// SHA256 is the content address of a blob.
type SHA256 [32]byte

// String returns the lower-case hex encoding.
func (s SHA256) String() string { return hex.EncodeToString(s[:]) }

// Bytes returns a copy suitable for a bytea column.
func (s SHA256) Bytes() []byte { b := make([]byte, 32); copy(b, s[:]); return b }

// SHA256FromBytes rebuilds a content address read back out of the database.
func SHA256FromBytes(b []byte) (SHA256, error) {
	var s SHA256
	if len(b) != 32 {
		return s, errors.New("blob: sha256 must be 32 bytes")
	}
	copy(s[:], b)
	return s, nil
}

// Store is the blob storage contract. It is one of the two deliberate
// single-implementation abstractions in v0: the filesystem implementation ships
// now and an S3 one follows, and every caller is written against this
// interface so that swap costs nothing.
type Store interface {
	// Put writes r and returns its content address and length. It is
	// idempotent: writing bytes that are already stored is a no-op that
	// returns the same address. Put returns only after the bytes are durable,
	// because the SMTP path must not acknowledge a message it could still
	// lose.
	Put(ctx context.Context, r io.Reader) (SHA256, int64, error)

	// Get opens a stored blob. The caller closes the reader.
	Get(ctx context.Context, sha SHA256) (io.ReadCloser, error)

	// Delete removes a blob. It is idempotent: deleting a blob that is not
	// there succeeds.
	Delete(ctx context.Context, sha SHA256) error

	// Locate returns the implementation-relative path recorded on the blob
	// row, so an operator can find the bytes without reverse-engineering the
	// sharding scheme.
	Locate(sha SHA256) string
}
