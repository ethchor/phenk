package pg

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
)

// fixtures creates one active domain per pool and returns them.
func fixtures(t *testing.T) (random, public *core.Domain) {
	t.Helper()
	ctx := context.Background()
	random = &core.Domain{Name: "rand.test", State: core.DomainActive, Pool: core.PoolRandom}
	public = &core.Domain{Name: "pub.test", State: core.DomainActive, Pool: core.PoolPublic}
	for _, d := range []*core.Domain{random, public} {
		if err := CreateDomain(ctx, testDB, d); err != nil {
			t.Fatalf("CreateDomain(%s): %v", d.Name, err)
		}
	}
	return random, public
}

func randomIdentity(domainID core.UUID, localPart string) *core.Identity {
	expires := time.Now().Add(time.Hour).UTC()
	return &core.Identity{
		LocalPart:      localPart,
		DomainID:       domainID,
		Kind:           core.KindRandom,
		State:          core.IdentityActive,
		OwnerSession:   "session-abc",
		WrappedDataKey: []byte("wrapped"),
		QuotaMessages:  core.DefaultRandomQuotaMessages,
		QuotaBytes:     core.DefaultRandomQuotaBytes,
		ExpiresAt:      &expires,
	}
}

func namedIdentity(domainID core.UUID, localPart string) *core.Identity {
	retention := core.DefaultNamedRetentionHours
	return &core.Identity{
		LocalPart:      localPart,
		DomainID:       domainID,
		Kind:           core.KindNamed,
		State:          core.IdentityActive,
		WrappedDataKey: []byte("wrapped"),
		RetentionHours: &retention,
		QuotaMessages:  core.DefaultNamedQuotaMessages,
		QuotaBytes:     core.DefaultNamedQuotaBytes,
	}
}

func TestIdentityCRUD(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)

	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	got, err := IdentityByID(ctx, testDB, id.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if got.LocalPart != "k7f2m9x3qz" || got.OwnerSession != "session-abc" || got.ExpiresAt == nil {
		t.Fatalf("round trip lost fields: %+v", got)
	}
	if got.DeliverySeq != 0 || got.UsedMessages != 0 || got.UsedBytes != 0 {
		t.Fatalf("a new identity should start empty: %+v", got)
	}

	byAddr, err := IdentityByAddress(ctx, testDB, "k7f2m9x3qz", rnd.ID)
	if err != nil || byAddr.ID != id.ID {
		t.Fatalf("IdentityByAddress: %+v %v", byAddr, err)
	}

	owned, err := IdentitiesByOwner(ctx, testDB, "session-abc")
	if err != nil || len(owned) != 1 || owned[0].ID != id.ID {
		t.Fatalf("IdentitiesByOwner: %+v %v", owned, err)
	}
	if none, _ := IdentitiesByOwner(ctx, testDB, "someone-else"); len(none) != 0 {
		t.Fatal("a session must not see another session's identities")
	}

	if _, err := IdentityByID(ctx, testDB, core.NewUUID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestAddressIsNeverReallocated(t *testing.T) {
	// Invariant 5. A purged address stays in the table as a tombstone and the
	// unique index covers every row, with no partial predicate, so the address
	// can never be handed out again.
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)

	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	if _, err := SetIdentityState(ctx, testDB, id.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	if _, err := DestroyDataKey(ctx, testDB, id.ID, time.Now().Add(90*24*time.Hour)); err != nil {
		t.Fatalf("DestroyDataKey: %v", err)
	}

	again := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, again); !errors.Is(err, ErrConflict) {
		t.Fatalf("reallocating a purged address: got %v, want ErrConflict", err)
	}
}

func TestDestroyDataKeyIsIdempotentAndGuarded(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)

	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	// An active identity is not purgeable: the guard is in the WHERE clause.
	moved, err := DestroyDataKey(ctx, testDB, id.ID, time.Now())
	if err != nil {
		t.Fatalf("DestroyDataKey: %v", err)
	}
	if moved {
		t.Fatal("purged an identity that was still active")
	}

	if _, err := SetIdentityState(ctx, testDB, id.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	reserved := time.Now().Add(90 * 24 * time.Hour).UTC()
	if moved, err = DestroyDataKey(ctx, testDB, id.ID, reserved); err != nil || !moved {
		t.Fatalf("DestroyDataKey: %v %v", moved, err)
	}

	got, err := IdentityByID(ctx, testDB, id.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if len(got.WrappedDataKey) != 0 {
		t.Fatal("the wrapped data key survived a purge")
	}
	if got.State != core.IdentityReserved || got.PurgedAt == nil || got.ReservedUntil == nil {
		t.Fatalf("after purge: %+v", got)
	}
	firstPurge := *got.PurgedAt

	// Running purge twice is safe and changes nothing.
	if moved, err = DestroyDataKey(ctx, testDB, id.ID, reserved); err != nil {
		t.Fatalf("second DestroyDataKey: %v", err)
	}
	if moved {
		t.Fatal("a second purge reported that it changed a row")
	}
	got, _ = IdentityByID(ctx, testDB, id.ID)
	if !got.PurgedAt.Equal(firstPurge) {
		t.Fatal("a second purge moved purged_at")
	}
}

func TestSetIdentityStateGuardsTheTransition(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)

	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	moved, err := SetIdentityState(ctx, testDB, id.ID, core.IdentityPurged, core.IdentityExpired)
	if err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	if moved {
		t.Fatal("moved an active identity from a state it was not in")
	}

	if moved, err = SetIdentityState(ctx, testDB, id.ID, core.IdentityExpired, core.IdentityActive); err != nil || !moved {
		t.Fatalf("SetIdentityState: %v %v", moved, err)
	}
	// Idempotent: the second run matches no row.
	if moved, err = SetIdentityState(ctx, testDB, id.ID, core.IdentityExpired, core.IdentityActive); err != nil || moved {
		t.Fatalf("repeat transition reported moved=%v err=%v", moved, err)
	}
}

func TestNamedIdentitiesCannotHoldAnOwnerOrAnExpiry(t *testing.T) {
	// Invariant 9, at the storage layer. The application check fires first and
	// names the rule; the database CHECK constraint is the backstop.
	reset(t)
	ctx := context.Background()
	_, pub := fixtures(t)

	withOwner := namedIdentity(pub.ID, "invoices")
	withOwner.OwnerSession = "session-abc"
	if err := CreateIdentity(ctx, testDB, withOwner); !errors.Is(err, core.ErrNotEligibleForAuth) {
		t.Fatalf("got %v, want ErrNotEligibleForAuth", err)
	}

	withExpiry := namedIdentity(pub.ID, "invoices")
	expires := time.Now().Add(time.Hour)
	withExpiry.ExpiresAt = &expires
	if err := CreateIdentity(ctx, testDB, withExpiry); err == nil {
		t.Fatal("created a named identity with an expiry")
	}

	// The database refuses it too, even if the application check is bypassed.
	_, err := testDB.Exec(ctx, `
		INSERT INTO identities (id, local_part, domain_id, kind, state, owner_session, wrapped_data_key)
		VALUES ($1, 'sneaky', $2, 'named', 'active', 'session-abc', 'k')`,
		core.NewUUID(), pub.ID)
	if err == nil {
		t.Fatal("the database accepted a named identity with an owner session")
	}
}

func TestCreateIdentityRequiresADataKey(t *testing.T) {
	// Invariant 4: every identity has a data encryption key.
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)

	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	id.WrappedDataKey = nil
	if err := CreateIdentity(ctx, testDB, id); err == nil {
		t.Fatal("created an identity with no data key")
	}
}

func TestCreateIdentityIfAbsentIsRaceSafe(t *testing.T) {
	// Two senders reaching a never-seen public name at the same moment must
	// produce exactly one identity, and exactly one of them must be told it
	// was the creator, because that is what decides who emits identity.created.
	reset(t)
	ctx := context.Background()
	_, pub := fixtures(t)

	const workers = 12
	type result struct {
		id      core.UUID
		created bool
		err     error
	}
	results := make(chan result, workers)
	start := make(chan struct{})

	for i := 0; i < workers; i++ {
		go func() {
			<-start
			stored, created, err := CreateIdentityIfAbsent(ctx, testDB, namedIdentity(pub.ID, "invoices"))
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{id: stored.ID, created: created}
		}()
	}
	close(start)

	var (
		creators int
		ids      = map[core.UUID]bool{}
	)
	for i := 0; i < workers; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("CreateIdentityIfAbsent: %v", r.err)
		}
		if r.created {
			creators++
		}
		ids[r.id] = true
	}
	if len(ids) != 1 {
		t.Fatalf("%d distinct identities created, want 1", len(ids))
	}
	if creators != 1 {
		t.Fatalf("%d callers reported creating the identity, want 1", creators)
	}

	var count int
	if err := testDB.QueryRow(ctx,
		`SELECT count(*) FROM identities WHERE local_part = 'invoices'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("%d rows for invoices@, want 1", count)
	}
}

func TestResolveRecipient(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, pub := fixtures(t)

	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	// Known address on a random domain.
	got, err := ResolveRecipient(ctx, testDB, "k7f2m9x3qz", "rand.test")
	if err != nil {
		t.Fatalf("ResolveRecipient: %v", err)
	}
	if got.Identity == nil || got.Identity.ID != id.ID || got.Domain.Pool != core.PoolRandom {
		t.Fatalf("got %+v", got)
	}

	// Unknown address on a known domain: the domain resolves, the identity is
	// nil, and the caller decides what that means from the pool.
	got, err = ResolveRecipient(ctx, testDB, "nobody", "pub.test")
	if err != nil {
		t.Fatalf("ResolveRecipient: %v", err)
	}
	if got.Identity != nil {
		t.Fatal("resolved an identity that does not exist")
	}
	if got.Domain.ID != pub.ID || got.Domain.Pool != core.PoolPublic {
		t.Fatalf("got domain %+v", got.Domain)
	}

	// Unknown domain: we are not the MX for it at all.
	if _, err := ResolveRecipient(ctx, testDB, "anyone", "elsewhere.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestBlockedLocalParts(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"postmaster", "abuse", "admin", "root", "security", "noreply", "no-reply", "support", "billing"} {
		blocked, err := IsLocalPartBlocked(ctx, testDB, name)
		if err != nil {
			t.Fatalf("IsLocalPartBlocked(%s): %v", name, err)
		}
		if !blocked {
			t.Errorf("%s should be blocked by the seeded denylist", name)
		}
	}
	if blocked, err := IsLocalPartBlocked(ctx, testDB, "invoices"); err != nil || blocked {
		t.Fatalf("invoices should not be blocked: %v %v", blocked, err)
	}

	if err := BlockLocalPart(ctx, testDB, "acmebank", "brand denylist"); err != nil {
		t.Fatalf("BlockLocalPart: %v", err)
	}
	if blocked, _ := IsLocalPartBlocked(ctx, testDB, "acmebank"); !blocked {
		t.Fatal("a newly blocked name is not blocked")
	}
	// Re-blocking is safe: the list is edited by hand and by scripts.
	if err := BlockLocalPart(ctx, testDB, "acmebank", "updated reason"); err != nil {
		t.Fatalf("repeat BlockLocalPart: %v", err)
	}
	_, _ = testDB.Exec(ctx, `DELETE FROM blocked_local_parts WHERE local_part = 'acmebank'`)
}
