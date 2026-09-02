package core

import "testing"

func TestKindPool(t *testing.T) {
	if KindRandom.Pool() != PoolRandom {
		t.Error("random identities must come from the random pool")
	}
	if KindNamed.Pool() != PoolPublic {
		t.Error("named identities must come from the public pool")
	}
}

func TestNamedIdentitiesAreNeverEligibleForScopedAccess(t *testing.T) {
	// Invariant 9. If this test ever fails, the shared-inbox feature has
	// reached into the agent security model.
	named := &Identity{Kind: KindNamed}
	if named.EligibleForScopedAccess() {
		t.Fatal("named identity reported eligible for scoped access")
	}
	random := &Identity{Kind: KindRandom}
	if !random.EligibleForScopedAccess() {
		t.Fatal("random identity reported ineligible for scoped access")
	}
}

func TestAcceptsMail(t *testing.T) {
	accepting := []IdentityState{IdentityActive, IdentityExpiring}
	for _, s := range accepting {
		if !s.AcceptsMail() {
			t.Errorf("state %q should accept mail", s)
		}
	}
	rejecting := []IdentityState{IdentityExpired, IdentityPurged, IdentityReserved}
	for _, s := range rejecting {
		if s.AcceptsMail() {
			t.Errorf("state %q must not accept mail", s)
		}
	}
}

func TestQuotaExceeded(t *testing.T) {
	id := &Identity{QuotaMessages: 2, QuotaBytes: 1000, UsedMessages: 1, UsedBytes: 900}
	if id.QuotaExceeded(100) {
		t.Error("a message that exactly fills the quota should be accepted")
	}
	if !id.QuotaExceeded(101) {
		t.Error("a message that overflows the byte quota should be refused")
	}
	id.UsedMessages = 2
	if !id.QuotaExceeded(1) {
		t.Error("a message past the count quota should be refused")
	}
}
