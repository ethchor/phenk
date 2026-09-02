package core

import (
	"net/netip"
	"time"
)

// Blob is an immutable, content-addressed byte stream: a raw MIME message or
// an extracted attachment. Blobs are shared by refcount, so one message
// delivered to two identities is stored once.
type Blob struct {
	ID        UUID
	SHA256    []byte
	SizeBytes int64
	Path      string
	Refcount  int
	CreatedAt time.Time
}

// DeliveryState tracks parsing, not acceptance. A delivery row exists only
// once the message is durably committed.
type DeliveryState string

const (
	// DeliveryReceived is committed but not yet parsed.
	DeliveryReceived DeliveryState = "received"
	// DeliveryParsed has structured output available.
	DeliveryParsed DeliveryState = "parsed"
	// DeliveryFailed could not be parsed. The raw message stays readable.
	DeliveryFailed DeliveryState = "failed"
)

// Valid reports whether s is a known delivery state.
func (s DeliveryState) Valid() bool {
	switch s {
	case DeliveryReceived, DeliveryParsed, DeliveryFailed:
		return true
	}
	return false
}

// AuthResult is the outcome of one mail authentication check.
type AuthResult string

const (
	AuthNone      AuthResult = "none"
	AuthPass      AuthResult = "pass"
	AuthFail      AuthResult = "fail"
	AuthSoftFail  AuthResult = "softfail"
	AuthNeutral   AuthResult = "neutral"
	AuthTempError AuthResult = "temperror"
	AuthPermError AuthResult = "permerror"
)

// Delivery is one accepted message for one identity.
type Delivery struct {
	ID         UUID
	IdentityID UUID

	// Seq is monotonic per identity and gapless. It is the cursor clients page
	// and wait on.
	Seq int64

	BlobID       UUID
	EnvelopeFrom string
	ClientIP     netip.Addr
	HELO         string
	TLS          bool
	SizeBytes    int64

	SPF   AuthResult
	DKIM  AuthResult
	DMARC AuthResult

	State      DeliveryState
	ReceivedAt time.Time
	ParsedAt   *time.Time

	// WrappedContentKey is the blob's content key, wrapped under this
	// identity's data key. The blob itself is shared between everyone who
	// received the same message; this is what makes each recipient's access to
	// it separately revocable, so purging one identity does not have to
	// destroy another's copy.
	WrappedContentKey []byte
}

// ParsedMessage is derived output. It is always rebuildable from the raw blob,
// so it can be dropped and regenerated at will.
type ParsedMessage struct {
	DeliveryID UUID
	Subject    string
	FromName   string
	FromAddr   string
	ToAddrs    []string
	SentAt     *time.Time

	// TextBody and HTMLBody are encrypted under the identity data key. The
	// HTML is sanitized before it is encrypted, never after it is decrypted,
	// so no code path can read unsanitized HTML out of the database.
	TextBody []byte
	HTMLBody []byte

	Preview string
}

// Attachment is one extracted part, stored as its own blob.
type Attachment struct {
	ID          UUID
	DeliveryID  UUID
	Filename    string
	ContentType string
	SizeBytes   int64
	BlobID      *UUID
}

// Event types emitted onto the events stream.
const (
	EventIdentityCreated  = "identity.created"
	EventIdentityExpiring = "identity.expiring"
	EventIdentityExpired  = "identity.expired"
	EventIdentityPurged   = "identity.purged"
	EventMessageReceived  = "message.received"
	EventMessageParsed    = "message.parsed"
	EventMessageExpired   = "message.expired"
)

// Event is an append-only record of something that happened to an identity.
type Event struct {
	Seq        int64
	IdentityID *UUID
	Type       string
	Payload    []byte
	CreatedAt  time.Time
}
