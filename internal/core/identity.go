package core

import "time"

// Kind distinguishes the two address kinds. See the v0 plan §6.5.
type Kind string

const (
	// KindRandom is an unguessable generated address owned by one session,
	// with a TTL, on a random-pool domain.
	KindRandom Kind = "random"
	// KindNamed is a chosen address shared by anyone who knows the name, with
	// no owner and no expiry, on a public-pool domain.
	KindNamed Kind = "named"
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { return k == KindRandom || k == KindNamed }

// Pool returns the domain pool a kind must be allocated from.
func (k Kind) Pool() Pool {
	if k == KindNamed {
		return PoolPublic
	}
	return PoolRandom
}

// IdentityState is the lifecycle state of an identity.
type IdentityState string

const (
	// IdentityActive accepts mail.
	IdentityActive IdentityState = "active"
	// IdentityExpiring is past its notice threshold but still accepting mail.
	IdentityExpiring IdentityState = "expiring"
	// IdentityExpired no longer accepts mail; its data is awaiting purge.
	IdentityExpired IdentityState = "expired"
	// IdentityPurged has had its data key destroyed.
	IdentityPurged IdentityState = "purged"
	// IdentityReserved is a permanent tombstone. The address is never
	// reallocated.
	IdentityReserved IdentityState = "reserved"
)

// Valid reports whether s is a known identity state.
func (s IdentityState) Valid() bool {
	switch s {
	case IdentityActive, IdentityExpiring, IdentityExpired, IdentityPurged, IdentityReserved:
		return true
	}
	return false
}

// AcceptsMail reports whether an identity in this state may be accepted at
// RCPT TO. Every other state is rejected with an identical 550, so that the
// SMTP surface leaks nothing about which addresses once existed.
func (s IdentityState) AcceptsMail() bool {
	return s == IdentityActive || s == IdentityExpiring
}

// Default quotas. Named inboxes are shared and far more exposed, so they get a
// smaller, rolling allowance.
const (
	DefaultRandomQuotaMessages = 200
	DefaultRandomQuotaBytes    = 100 << 20
	DefaultNamedQuotaMessages  = 50
	DefaultNamedQuotaBytes     = 25 << 20

	// DefaultNamedRetentionHours is the rolling per-delivery retention for
	// named inboxes.
	DefaultNamedRetentionHours = 168
)

// Identity is an address plus everything that governs its life: who owns it,
// when it dies, how much it may hold, and the wrapped key its contents are
// encrypted under.
type Identity struct {
	ID        UUID
	LocalPart string
	DomainID  UUID
	Kind      Kind
	State     IdentityState

	// OwnerSession is the opaque session cookie value for random identities,
	// and is always empty for named ones: a shared inbox can have no owner.
	OwnerSession string

	// WrappedDataKey is the identity's data encryption key, wrapped under the
	// master key. Purge destroys it, which destroys the contents with it.
	WrappedDataKey []byte

	// RetentionHours is the rolling per-delivery retention for named
	// identities, and is nil for random ones, whose retention is the identity
	// TTL itself.
	RetentionHours *int

	DeliverySeq int64

	QuotaMessages int
	QuotaBytes    int64
	UsedMessages  int
	UsedBytes     int64

	CreatedAt time.Time
	// ExpiresAt is nil for named identities: the inbox is perpetual.
	ExpiresAt     *time.Time
	PurgedAt      *time.Time
	ReservedUntil *time.Time
}

// QuotaExceeded reports whether accepting a message of size bytes would put the
// identity over either quota.
func (i *Identity) QuotaExceeded(size int64) bool {
	return i.UsedMessages+1 > i.QuotaMessages || i.UsedBytes+size > i.QuotaBytes
}

// EligibleForScopedAccess reports whether an identity may be the subject of a
// grant, a webhook target, or an API key. Named identities never are: they are
// shared by construction, so scoped access to one would be scoped access to
// nothing. This is invariant 9 of the v0 plan, and it is the single rule that
// keeps the shared-inbox feature out of the agent security model.
func (i *Identity) EligibleForScopedAccess() bool { return i.Kind == KindRandom }
