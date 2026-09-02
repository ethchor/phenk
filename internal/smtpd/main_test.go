package smtpd

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/testsupport/pgtest"
)

var testDB *pg.DB

func TestMain(m *testing.M) {
	// The listener logs every accepted message and every refusal, which is
	// right in production and unreadable in a test run.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Each test package gets its own database: `go test ./...` runs packages in
	// parallel and every storage test truncates the tables it uses, so one
	// shared database means one package deleting another package's fixtures
	// halfway through a test.
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "smtpd")
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
	os.Stderr.WriteString("smtpd: skipping database tests: " + err.Error() + "\n")
	// The tests that need no database still run.
	os.Exit(runSuite(m))
}

// harness is a running listener over the real storage layer.
type harness struct {
	t         *testing.T
	db        *pg.DB
	blobs     *blob.FS
	blobDir   string
	keyring   *crypto.Keyring
	allocator *alloc.Allocator
	server    *Server
	addr      string

	randomDomain core.Domain
	publicDomain core.Domain
}

// newHarness starts a listener on a random port with one active domain in each
// pool, and an empty database.
func newHarness(t *testing.T, configure ...func(*Config)) *harness {
	t.Helper()
	if testDB == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()

	if _, err := testDB.Exec(ctx, `
		TRUNCATE attachments, parsed_messages, deliveries, events, identities, blobs, domains RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset: %v", err)
	}

	master, err := crypto.ParseMasterKey(crypto.GenerateMasterKey())
	if err != nil {
		t.Fatalf("ParseMasterKey: %v", err)
	}
	keyring, err := crypto.NewKeyring(master)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	blobDir := t.TempDir()
	blobs, err := blob.NewFS(blobDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	allocator := alloc.New(keyring, alloc.Options{
		DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, NamedRetention: 168 * time.Hour,
	})

	h := &harness{
		t: t, db: testDB, blobs: blobs, blobDir: blobDir,
		keyring: keyring, allocator: allocator,
	}

	h.randomDomain = h.addDomain("rand.test", core.PoolRandom)
	h.publicDomain = h.addDomain("pub.test", core.PoolPublic)

	cfg := Config{
		Hostname:              "mx.test",
		MaxMessageBytes:       25 << 20,
		MaxPublicMessageBytes: 10 << 20,
		MaxRecipients:         10,
		MaxConnectionsPerIP:   10,
		IdleTimeout:           10 * time.Second,
		SpoolDir:              t.TempDir(),
		// The cache is disabled by default in tests: every assertion here is
		// about what the database contains, and a cache would make some of
		// them depend on timing. The cache has its own tests.
		ResolveCacheTTL:      time.Nanosecond,
		ProvisionsPerIPHour:  1000,
		GlobalProvisionsHour: 1000,
	}
	for _, fn := range configure {
		fn(&cfg)
	}

	h.server = New(cfg, testDB, blobs, allocator)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	h.addr = listener.Addr().String()

	serveCtx, cancel := context.WithCancel(ctx)
	served := make(chan error, 1)
	go func() { served <- h.server.Serve(serveCtx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("smtp server did not shut down")
		}
	})

	return h
}

func (h *harness) addDomain(name string, pool core.Pool) core.Domain {
	h.t.Helper()
	d := &core.Domain{Name: name, State: core.DomainActive, Pool: pool}
	if err := pg.CreateDomain(context.Background(), h.db, d); err != nil {
		h.t.Fatalf("CreateDomain(%s): %v", name, err)
	}
	return *d
}

// allocate mints a random identity and returns its address.
func (h *harness) allocate(session string) (*core.Identity, string) {
	h.t.Helper()
	result, err := h.allocator.AllocateRandom(context.Background(), h.db, session, time.Hour)
	if err != nil {
		h.t.Fatalf("AllocateRandom: %v", err)
	}
	return result.Identity, result.Address()
}

func (h *harness) countRows(table string) int {
	h.t.Helper()
	var n int
	if err := h.db.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		h.t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

func (h *harness) deliveries(identityID core.UUID) []core.Delivery {
	h.t.Helper()
	got, err := pg.DeliveriesSince(context.Background(), h.db, identityID, 0, 1000)
	if err != nil {
		h.t.Fatalf("DeliveriesSince: %v", err)
	}
	return got
}

// runSuite exists so TestMain has a single exit point whether or not a
// database was available.
func runSuite(m *testing.M) int { return m.Run() }
