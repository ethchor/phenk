package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

func TestNoticeWarnsOnceAndOnlyOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(10 * time.Minute)

	// Too early: nothing is within the notice window yet.
	if n, err := f.runner.Notice(ctx); err != nil || n != 0 {
		t.Fatalf("early Notice: %d %v", n, err)
	}

	f.advance(6 * time.Minute)
	if n, err := f.runner.Notice(ctx); err != nil || n != 1 {
		t.Fatalf("Notice: %d %v", n, err)
	}
	if state := f.reload(identity.ID).State; state != core.IdentityExpiring {
		t.Fatalf("state = %q, want expiring", state)
	}
	// An expiring address still accepts mail: the notice is a warning, not a
	// cut-off.
	if !f.reload(identity.ID).State.AcceptsMail() {
		t.Fatal("an expiring address stopped accepting mail")
	}

	// Running it again warns nobody twice.
	if n, err := f.runner.Notice(ctx); err != nil || n != 0 {
		t.Fatalf("repeat Notice: %d %v", n, err)
	}
	if got := f.countEvents(core.EventIdentityExpiring); got != 1 {
		t.Fatalf("%d expiring events, want 1", got)
	}
}

func TestNoticeSkipsNamedIdentities(t *testing.T) {
	// A named inbox is perpetual and has no expiry to warn about.
	f := newFixture(t)
	ctx := context.Background()
	named := f.named("invoices")

	f.advance(365 * 24 * time.Hour)
	if n, err := f.runner.Notice(ctx); err != nil || n != 0 {
		t.Fatalf("Notice: %d %v", n, err)
	}
	if state := f.reload(named.ID).State; state != core.IdentityActive {
		t.Fatalf("a named inbox moved to %q", state)
	}
}

func TestExpireIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(time.Hour)

	if n, err := f.runner.Expire(ctx); err != nil || n != 0 {
		t.Fatalf("early Expire: %d %v", n, err)
	}

	f.advance(2 * time.Hour)
	if n, err := f.runner.Expire(ctx); err != nil || n != 1 {
		t.Fatalf("Expire: %d %v", n, err)
	}
	if state := f.reload(identity.ID).State; state != core.IdentityExpired {
		t.Fatalf("state = %q, want expired", state)
	}
	// An expired address stops accepting mail immediately, before anything is
	// destroyed.
	if f.reload(identity.ID).State.AcceptsMail() {
		t.Fatal("an expired address still accepts mail")
	}

	if n, err := f.runner.Expire(ctx); err != nil || n != 0 {
		t.Fatalf("repeat Expire: %d %v", n, err)
	}
	if got := f.countEvents(core.EventIdentityExpired); got != 1 {
		t.Fatalf("%d expired events, want 1", got)
	}
}

func TestExpireCatchesAnIdentityAlreadyWarned(t *testing.T) {
	// The progression is active → expiring → expired, and nothing may be
	// stranded in the middle of it.
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(6 * time.Minute)

	f.advance(2 * time.Minute)
	if _, err := f.runner.Notice(ctx); err != nil {
		t.Fatalf("Notice: %v", err)
	}
	if state := f.reload(identity.ID).State; state != core.IdentityExpiring {
		t.Fatalf("state = %q, want expiring", state)
	}

	f.advance(10 * time.Minute)
	if n, err := f.runner.Expire(ctx); err != nil || n != 1 {
		t.Fatalf("Expire: %d %v", n, err)
	}
	if state := f.reload(identity.ID).State; state != core.IdentityExpired {
		t.Fatalf("state = %q, want expired", state)
	}
}

func TestPurgeDestroysContentAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(time.Hour)
	f.deliver(identity, "Subject: private\r\n\r\nsecret", f.clock)

	if f.countRows("deliveries") != 1 || f.blobFiles() != 1 {
		t.Fatal("the delivery was not set up")
	}

	// Just past the deadline, so the grace window is still open.
	f.advance(time.Hour + time.Minute)
	if _, err := f.runner.Expire(ctx); err != nil {
		t.Fatalf("Expire: %v", err)
	}

	// The grace window is deliberate: a client mid-request when the deadline
	// passes should not have a message vanish underneath it.
	if n, err := f.runner.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("Purge inside the grace window: %d %v", n, err)
	}

	f.advance(10 * time.Minute)
	if n, err := f.runner.Purge(ctx); err != nil || n != 1 {
		t.Fatalf("Purge: %d %v", n, err)
	}

	purged := f.reload(identity.ID)
	if purged.State != core.IdentityReserved {
		t.Fatalf("state = %q, want reserved", purged.State)
	}
	if len(purged.WrappedDataKey) != 0 {
		t.Fatal("the data key survived the purge")
	}
	if purged.PurgedAt == nil || purged.ReservedUntil == nil {
		t.Fatalf("purged identity = %+v", purged)
	}
	// The key is what destroys the data, and it is gone.
	if _, err := f.keyring.Unwrap(purged.ID, purged.WrappedDataKey); err == nil {
		t.Fatal("a purged identity's key still unwraps")
	}
	if f.countRows("deliveries") != 0 || f.countRows("parsed_messages") != 0 {
		t.Fatal("messages survived the purge")
	}
	if f.countRows("blobs") != 0 || f.blobFiles() != 0 {
		t.Fatalf("%d blob rows and %d files survived the purge", f.countRows("blobs"), f.blobFiles())
	}

	firstPurgedAt := *purged.PurgedAt
	if n, err := f.runner.Purge(ctx); err != nil || n != 0 {
		t.Fatalf("repeat Purge: %d %v", n, err)
	}
	if again := f.reload(identity.ID); !again.PurgedAt.Equal(firstPurgedAt) {
		t.Fatal("a second purge moved purged_at")
	}
	if got := f.countEvents(core.EventIdentityPurged); got != 1 {
		t.Fatalf("%d purged events, want 1", got)
	}
}

func TestPurgeLeavesTheAddressUnallocatableForever(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(time.Hour)

	f.advance(2 * time.Hour)
	if _, err := f.runner.Expire(ctx); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	f.advance(10 * time.Minute)
	if _, err := f.runner.Purge(ctx); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	// Long past the reservation period, the tombstone is still there and the
	// address still cannot be handed to anyone else.
	f.advance(400 * 24 * time.Hour)
	if _, err := f.runner.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if f.countRows("identities") != 1 {
		t.Fatal("the tombstone was deleted")
	}

	taken := &core.Identity{
		LocalPart: identity.LocalPart, DomainID: identity.DomainID,
		Kind: core.KindRandom, State: core.IdentityActive,
		WrappedDataKey: []byte("k"), QuotaMessages: 1, QuotaBytes: 1,
	}
	if err := pg.CreateIdentity(ctx, f.db, taken); err == nil {
		t.Fatal("a purged address was reallocated")
	}
}

func TestPurgeKeepsBlobsAnotherIdentityStillHolds(t *testing.T) {
	// One message to two identities is one blob. Purging one of them must not
	// take the other's copy with it.
	f := newFixture(t)
	ctx := context.Background()

	purged := f.allocate(time.Hour)
	kept := f.allocate(10 * time.Hour)
	// Delivered in one transaction, as a single SMTP message with two
	// recipients is: one set of bytes, one content key, two wrappings.
	f.deliverShared([]*core.Identity{purged, kept}, "Subject: shared\r\n\r\nthe same bytes", f.clock)

	if f.countRows("blobs") != 1 || f.blobFiles() != 1 {
		t.Fatalf("%d blob rows and %d files, want one of each", f.countRows("blobs"), f.blobFiles())
	}

	f.advance(2 * time.Hour)
	if _, err := f.runner.Expire(ctx); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	f.advance(10 * time.Minute)
	if n, err := f.runner.Purge(ctx); err != nil || n != 1 {
		t.Fatalf("Purge: %d %v", n, err)
	}

	if f.countRows("blobs") != 1 || f.blobFiles() != 1 {
		t.Fatal("purging one recipient destroyed a blob the other still holds")
	}
	remaining, err := pg.DeliveriesSince(ctx, f.db, kept.ID, 0, 10)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("the other identity lost its message: %d %v", len(remaining), err)
	}
}

func TestSweepAppliesRollingRetention(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	named := f.named("invoices")

	old := f.clock.Add(-200 * time.Hour)
	recent := f.clock.Add(-1 * time.Hour)
	f.deliver(named, "old message", old)
	f.deliver(named, "recent message", recent)

	if n, err := f.runner.Sweep(ctx); err != nil || n != 1 {
		t.Fatalf("Sweep: %d %v", n, err)
	}
	remaining, err := pg.DeliveriesSince(ctx, f.db, named.ID, 0, 10)
	if err != nil {
		t.Fatalf("DeliveriesSince: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Seq != 2 {
		t.Fatalf("remaining = %+v, want only the recent message", remaining)
	}

	// The identity survives with its key: a named inbox is permanent even when
	// its messages are not.
	identity := f.reload(named.ID)
	if identity.State != core.IdentityActive || len(identity.WrappedDataKey) == 0 {
		t.Fatalf("the inbox itself was damaged: %+v", identity)
	}
	// And its usage came down, or a swept inbox would stay permanently full.
	if identity.UsedMessages != 1 {
		t.Fatalf("used_messages = %d, want 1", identity.UsedMessages)
	}
	if got := f.countEvents(core.EventMessageExpired); got != 1 {
		t.Fatalf("%d message.expired events, want 1", got)
	}

	// Running it again removes nothing more and emits nothing more.
	if n, err := f.runner.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("repeat Sweep: %d %v", n, err)
	}
	if got := f.countEvents(core.EventMessageExpired); got != 1 {
		t.Fatalf("%d message.expired events after a second sweep, want 1", got)
	}
}

func TestSweepEnforcesTheMessageQuota(t *testing.T) {
	// A named inbox is shared, so one noisy sender must not be able to push
	// everyone else's mail out by volume alone.
	f := newFixture(t)
	ctx := context.Background()
	named := f.named("invoices")

	if _, err := f.db.Exec(ctx, `UPDATE identities SET quota_messages = 3 WHERE id = $1`, named.ID); err != nil {
		t.Fatalf("setting quota: %v", err)
	}
	for i := 0; i < 6; i++ {
		f.deliver(named, "message", f.clock)
	}

	if n, err := f.runner.Sweep(ctx); err != nil || n != 3 {
		t.Fatalf("Sweep: %d %v", n, err)
	}
	remaining, err := pg.DeliveriesSince(ctx, f.db, named.ID, 0, 10)
	if err != nil {
		t.Fatalf("DeliveriesSince: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("%d messages remain, want 3", len(remaining))
	}
	// The oldest go first: they are the least likely to still be wanted.
	if remaining[0].Seq != 4 {
		t.Fatalf("oldest remaining seq = %d, want 4", remaining[0].Seq)
	}
}

func TestSweepSkipsRandomIdentities(t *testing.T) {
	// Random addresses have a TTL. Their retention is the identity's own life,
	// and the rolling sweep must not touch them.
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(10 * time.Hour)
	f.deliver(identity, "ancient", f.clock.Add(-1000*time.Hour))

	if n, err := f.runner.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("Sweep: %d %v", n, err)
	}
	if f.countRows("deliveries") != 1 {
		t.Fatal("the sweep deleted a random identity's message")
	}
}

func TestCollectOrphansRemovesUnreferencedBlobs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	sha, size, err := f.blobs.Put(ctx, bytesReader("orphaned bytes"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	var blobID core.UUID
	if err := f.db.InTx(ctx, func(q pg.Querier) error {
		id, _, err := pg.AcquireBlob(ctx, q, sha, size, f.blobs.Locate(sha))
		blobID = id
		return err
	}); err != nil {
		t.Fatalf("AcquireBlob: %v", err)
	}
	if _, err := pg.ReleaseBlob(ctx, f.db, blobID); err != nil {
		t.Fatalf("ReleaseBlob: %v", err)
	}

	if n, err := f.runner.CollectOrphans(ctx); err != nil || n != 1 {
		t.Fatalf("CollectOrphans: %d %v", n, err)
	}
	if f.countRows("blobs") != 0 || f.blobFiles() != 0 {
		t.Fatalf("%d rows and %d files survived collection", f.countRows("blobs"), f.blobFiles())
	}

	// Running it again finds nothing and fails at nothing.
	if n, err := f.runner.CollectOrphans(ctx); err != nil || n != 0 {
		t.Fatalf("repeat CollectOrphans: %d %v", n, err)
	}
}

func TestCollectOrphansKeepsAReferencedBlob(t *testing.T) {
	// A delivery that acquires a blob between the scan and the delete keeps it:
	// the refcount is re-checked under a row lock.
	f := newFixture(t)
	ctx := context.Background()
	identity := f.allocate(time.Hour)
	f.deliver(identity, "still referenced", f.clock)

	if n, err := f.runner.CollectOrphans(ctx); err != nil || n != 0 {
		t.Fatalf("CollectOrphans: %d %v", n, err)
	}
	if f.countRows("blobs") != 1 || f.blobFiles() != 1 {
		t.Fatal("a referenced blob was collected")
	}
}

func TestEveryJobRunsTwiceWithIdenticalOutcome(t *testing.T) {
	// The acceptance criterion for this phase: run each job twice and assert
	// identical final state and no duplicate events.
	f := newFixture(t)
	ctx := context.Background()

	expiring := f.allocate(6 * time.Minute)
	expired := f.allocate(time.Minute)
	named := f.named("invoices")
	f.deliver(expired, "message for a doomed address", f.clock)
	f.deliver(named, "old public message", f.clock.Add(-200*time.Hour))
	f.deliver(named, "recent public message", f.clock)

	f.advance(2 * time.Minute)

	type snapshot struct {
		identities, deliveries, blobs, parsed int
		files                                 int
		states                                map[core.UUID]core.IdentityState
		events                                map[string]int
	}
	take := func() snapshot {
		s := snapshot{
			identities: f.countRows("identities"),
			deliveries: f.countRows("deliveries"),
			blobs:      f.countRows("blobs"),
			parsed:     f.countRows("parsed_messages"),
			files:      f.blobFiles(),
			states:     map[core.UUID]core.IdentityState{},
			events:     map[string]int{},
		}
		for _, id := range []core.UUID{expiring.ID, expired.ID, named.ID} {
			s.states[id] = f.reload(id).State
		}
		for _, eventType := range []string{
			core.EventIdentityExpiring, core.EventIdentityExpired,
			core.EventIdentityPurged, core.EventMessageExpired,
		} {
			s.events[eventType] = f.countEvents(eventType)
		}
		return s
	}

	runAll := func() {
		if _, err := f.runner.Notice(ctx); err != nil {
			t.Fatalf("Notice: %v", err)
		}
		if _, err := f.runner.Expire(ctx); err != nil {
			t.Fatalf("Expire: %v", err)
		}
		if _, err := f.runner.Purge(ctx); err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if _, err := f.runner.Sweep(ctx); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if _, err := f.runner.CollectOrphans(ctx); err != nil {
			t.Fatalf("CollectOrphans: %v", err)
		}
		if _, err := f.runner.Release(ctx); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}

	// First pass, then move past the purge grace and run again so purge has
	// something to do, then a third pass that must change nothing at all.
	runAll()
	f.advance(10 * time.Minute)
	runAll()

	first := take()
	runAll()
	second := take()

	if first.identities != second.identities || first.deliveries != second.deliveries ||
		first.blobs != second.blobs || first.parsed != second.parsed || first.files != second.files {
		t.Fatalf("row counts changed on a repeat run:\n%+v\n%+v", first, second)
	}
	for id, state := range first.states {
		if second.states[id] != state {
			t.Fatalf("identity %s moved from %q to %q on a repeat run", id, state, second.states[id])
		}
	}
	for eventType, count := range first.events {
		if second.events[eventType] != count {
			t.Fatalf("%s went from %d to %d events on a repeat run",
				eventType, count, second.events[eventType])
		}
	}

	// And the run actually did something the first time, or the test proves
	// nothing.
	if first.events[core.EventIdentityExpired] == 0 || first.events[core.EventIdentityPurged] == 0 {
		t.Fatalf("the jobs did no work: %+v", first.events)
	}
}
