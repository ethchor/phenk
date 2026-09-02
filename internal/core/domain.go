package core

import "time"

// Pool separates domains by the kind of identity they serve. The pools never
// mix: public domains attract far more spam and get blocklisted faster, and
// keeping them apart contains reputation damage to the pool that earned it.
type Pool string

const (
	// PoolRandom serves generated, session-owned identities.
	PoolRandom Pool = "random"
	// PoolPublic serves shared named inboxes.
	PoolPublic Pool = "public"
)

// Valid reports whether p is a known pool.
func (p Pool) Valid() bool { return p == PoolRandom || p == PoolPublic }

// DomainState tracks a domain through its reputation lifecycle.
type DomainState string

const (
	// DomainFresh is registered and warming, not yet handing out addresses.
	DomainFresh DomainState = "fresh"
	// DomainActive is accepting new allocations.
	DomainActive DomainState = "active"
	// DomainBurned is blocklisted: it still receives mail for existing
	// identities but hands out no new addresses.
	DomainBurned DomainState = "burned"
	// DomainRetired no longer receives mail at all.
	DomainRetired DomainState = "retired"
)

// Valid reports whether s is a known domain state.
func (s DomainState) Valid() bool {
	switch s {
	case DomainFresh, DomainActive, DomainBurned, DomainRetired:
		return true
	}
	return false
}

// Domain is a reputation-bearing, rotating resource.
type Domain struct {
	ID        UUID
	Name      string
	State     DomainState
	Pool      Pool
	CreatedAt time.Time
	BurnedAt  *time.Time
}
