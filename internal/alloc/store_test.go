package alloc

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/testsupport/pgtest"
)

var testDB *pg.DB

func TestMain(m *testing.M) {
	// Each test package gets its own database: `go test ./...` runs packages in
	// parallel and every storage test truncates the tables it uses, so one
	// shared database means one package deleting another package's fixtures
	// halfway through a test.
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "alloc")
	cancelSetup()
	if err == nil {
		var db *pg.DB
		openCtx, cancelOpen := context.WithTimeout(context.Background(), 15*time.Second)
		db, err = pg.Open(openCtx, dsn, 16)
		cancelOpen()
		if err == nil {
			defer db.Close()
			migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 60*time.Second)
			err = db.Migrate(migrateCtx)
			cancelMigrate()
			if err == nil {
				testDB = db
				os.Exit(runSuite(m))
			}
		}
	}

	if pgtest.Required() {
		panic("PHENK_TEST_REQUIRED is set but the test database is unusable: " + err.Error())
	}
	os.Stderr.WriteString("alloc: skipping database tests: " + err.Error() + "\n")
	// The tests that need no database still run.
	os.Exit(runSuite(m))
}

func requireDB(t *testing.T) *pg.DB {
	t.Helper()
	if testDB == nil {
		t.Skip("no test database")
	}
	_, err := testDB.Exec(context.Background(), `
		TRUNCATE attachments, parsed_messages, deliveries, events, identities, blobs, domains RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	return testDB
}

func testKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	master, err := crypto.ParseMasterKey(crypto.GenerateMasterKey())
	if err != nil {
		t.Fatalf("ParseMasterKey: %v", err)
	}
	kr, err := crypto.NewKeyring(master)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return kr
}

func testAllocator(t *testing.T) *Allocator {
	t.Helper()
	return New(testKeyring(t), Options{DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, NamedRetention: 168 * time.Hour})
}

func seedDomains(t *testing.T, db *pg.DB, names ...string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range names {
		pool := core.PoolRandom
		state := core.DomainActive
		switch {
		case len(name) > 5 && name[:5] == "pub__":
			pool, name = core.PoolPublic, name[5:]
		case len(name) > 6 && name[:6] == "fresh_":
			state, name = core.DomainFresh, name[6:]
		}
		d := &core.Domain{Name: name, State: state, Pool: pool}
		if err := pg.CreateDomain(ctx, db, d); err != nil {
			t.Fatalf("CreateDomain(%s): %v", name, err)
		}
	}
}

func TestAllocateRandom(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "rand.test")
	a := testAllocator(t)

	got, err := a.AllocateRandom(ctx, db, "session-1", time.Hour)
	if err != nil {
		t.Fatalf("AllocateRandom: %v", err)
	}
	if !core.LooksGenerated(got.Identity.LocalPart) {
		t.Fatalf("allocated %q, which is not a generated address", got.Identity.LocalPart)
	}
	if got.Domain.Name != "rand.test" || got.Address() != got.Identity.LocalPart+"@rand.test" {
		t.Fatalf("address = %q", got.Address())
	}
	if got.Identity.Kind != core.KindRandom || got.Identity.OwnerSession != "session-1" {
		t.Fatalf("identity = %+v", got.Identity)
	}
	if got.Identity.ExpiresAt == nil {
		t.Fatal("a random identity must expire")
	}
	if len(got.Identity.WrappedDataKey) == 0 {
		t.Fatal("a random identity must have a data key")
	}

	// The identity is durable and the creation event was emitted with it.
	stored, err := pg.IdentityByID(ctx, db, got.Identity.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if stored.LocalPart != got.Identity.LocalPart {
		t.Fatal("the allocated identity was not committed")
	}
	events, err := pg.EventsForIdentity(ctx, db, got.Identity.ID, 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != core.EventIdentityCreated {
		t.Fatalf("events = %+v, err %v", events, err)
	}
}

func TestAllocateRandomClampsTTL(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "rand.test")
	a := testAllocator(t)

	long, err := a.AllocateRandom(ctx, db, "session-1", 30*24*time.Hour)
	if err != nil {
		t.Fatalf("AllocateRandom: %v", err)
	}
	if time.Until(*long.Identity.ExpiresAt) > 25*time.Hour {
		t.Fatalf("ttl was not clamped: expires in %v", time.Until(*long.Identity.ExpiresAt))
	}

	zero, err := a.AllocateRandom(ctx, db, "session-1", 0)
	if err != nil {
		t.Fatalf("AllocateRandom: %v", err)
	}
	if d := time.Until(*zero.Identity.ExpiresAt); d > 61*time.Minute || d < 59*time.Minute {
		t.Fatalf("a zero ttl should take the default hour, got %v", d)
	}
}

func TestAllocateRandomNeedsAnActiveDomain(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	// A fresh domain is warming and a public one is the wrong pool: neither can
	// serve a generated address.
	seedDomains(t, db, "fresh_warming.test", "pub__pub.test")
	a := testAllocator(t)

	if _, err := a.AllocateRandom(ctx, db, "session-1", time.Hour); !errors.Is(err, ErrNoDomains) {
		t.Fatalf("got %v, want ErrNoDomains", err)
	}
}

func TestResolveOrCreateNamedIsStable(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "pub__a.test", "pub__b.test", "pub__c.test")
	a := testAllocator(t)

	first, err := a.ResolveOrCreateNamed(ctx, db, "Invoices+ebay")
	if err != nil {
		t.Fatalf("ResolveOrCreateNamed: %v", err)
	}
	if first.Identity.LocalPart != "invoices" {
		t.Fatalf("local part = %q, want the normalized name", first.Identity.LocalPart)
	}
	if !first.Created || first.Identity.Kind != core.KindNamed {
		t.Fatalf("first resolve: %+v", first)
	}
	if first.Identity.OwnerSession != "" || first.Identity.ExpiresAt != nil {
		t.Fatal("a named inbox must have no owner and no expiry")
	}
	if first.Identity.RetentionHours == nil || *first.Identity.RetentionHours != 168 {
		t.Fatalf("retention = %v, want 168 hours", first.Identity.RetentionHours)
	}

	// The same name must come back as the same address, every time. An address
	// a user pastes into a signup form has to still be theirs tomorrow.
	for i := 0; i < 5; i++ {
		again, err := a.ResolveOrCreateNamed(ctx, db, "invoices")
		if err != nil {
			t.Fatalf("ResolveOrCreateNamed: %v", err)
		}
		if again.Address() != first.Address() {
			t.Fatalf("resolve %d gave %q, want %q", i, again.Address(), first.Address())
		}
		if again.Created {
			t.Fatal("an existing inbox was reported as newly created")
		}
	}
}

func TestResolveOrCreateNamedRejectsInvalidAndBlockedNames(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "pub__pub.test")
	a := testAllocator(t)

	if _, err := a.ResolveOrCreateNamed(ctx, db, "ab"); !errors.Is(err, core.ErrLocalPartSyntax) {
		t.Fatalf("short name: got %v, want ErrLocalPartSyntax", err)
	}
	if _, err := a.ResolveOrCreateNamed(ctx, db, "k7f2m9x3qz"); !errors.Is(err, core.ErrLocalPartReserved) {
		t.Fatalf("generated shape: got %v, want ErrLocalPartReserved", err)
	}
	if _, err := a.ResolveOrCreateNamed(ctx, db, "admin"); !errors.Is(err, core.ErrLocalPartBlocked) {
		t.Fatalf("blocked name: got %v, want ErrLocalPartBlocked", err)
	}

	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM identities`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d identities created by rejected names", count)
	}
}

func TestProvisionNamedRefusesRandomPoolDomains(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "rand.test")
	a := testAllocator(t)

	domain, err := pg.DomainByName(ctx, db, "rand.test")
	if err != nil {
		t.Fatalf("DomainByName: %v", err)
	}
	// The pools never mix: a named inbox cannot be created on a random-pool
	// domain, where it could shadow a generated address.
	if _, err := a.ProvisionNamed(ctx, db, domain, "invoices"); !errors.Is(err, core.ErrKindPoolMismatch) {
		t.Fatalf("got %v, want ErrKindPoolMismatch", err)
	}
}

func TestSelectNamedDomainIsDeterministicAndSpread(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "pub__a.test", "pub__b.test", "pub__c.test")

	first, err := SelectNamedDomain(ctx, db, "invoices")
	if err != nil {
		t.Fatalf("SelectNamedDomain: %v", err)
	}
	for i := 0; i < 10; i++ {
		again, err := SelectNamedDomain(ctx, db, "invoices")
		if err != nil || again.Name != first.Name {
			t.Fatalf("selection is not deterministic: %v vs %v (%v)", again, first, err)
		}
	}

	// Different names should not all pile onto one domain.
	used := map[string]bool{}
	for _, name := range []string{"invoices", "newsletters", "receipts", "signups", "shopping", "alerts", "travel", "banking"} {
		d, err := SelectNamedDomain(ctx, db, name)
		if err != nil {
			t.Fatalf("SelectNamedDomain(%s): %v", name, err)
		}
		used[d.Name] = true
	}
	if len(used) < 2 {
		t.Fatalf("every name hashed to the same domain: %v", used)
	}
}

func TestConcurrentProvisionCreatesOneIdentity(t *testing.T) {
	// The race the plan calls out: two simultaneous senders to the same
	// never-seen name.
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "pub__pub.test")
	a := testAllocator(t)

	const workers = 12
	type outcome struct {
		address string
		created bool
		err     error
	}
	results := make(chan outcome, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			r, err := a.ResolveOrCreateNamed(ctx, db, "invoices")
			if err != nil {
				results <- outcome{err: err}
				return
			}
			results <- outcome{address: r.Address(), created: r.Created}
		}()
	}
	close(start)

	creators := 0
	addresses := map[string]bool{}
	for i := 0; i < workers; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("ResolveOrCreateNamed: %v", r.err)
		}
		if r.created {
			creators++
		}
		addresses[r.address] = true
	}
	if len(addresses) != 1 {
		t.Fatalf("got %d distinct addresses, want 1: %v", len(addresses), addresses)
	}
	if creators != 1 {
		t.Fatalf("%d callers claimed to create the inbox, want 1", creators)
	}

	var identities, events int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM identities`).Scan(&identities)
	_ = db.QueryRow(ctx, `SELECT count(*) FROM events WHERE type = $1`, core.EventIdentityCreated).Scan(&events)
	if identities != 1 {
		t.Fatalf("%d identity rows, want 1", identities)
	}
	if events != 1 {
		t.Fatalf("%d identity.created events, want 1", events)
	}
}

// mustAddr parses an IP that is known good at compile time.
func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

// runSuite exists so TestMain has a single exit point whether or not a
// database was available.
func runSuite(m *testing.M) int { return m.Run() }
