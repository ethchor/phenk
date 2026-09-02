package lifecycle

import (
	"bytes"
	"context"
	"io"
	"log/slog"
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
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "lifecycle")
	cancelSetup()
	if err == nil {
		var db *pg.DB
		openCtx, cancelOpen := context.WithTimeout(context.Background(), 15*time.Second)
		db, err = pg.Open(openCtx, dsn, 8)
		cancelOpen()
		if err == nil {
			defer db.Close()
			migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 60*time.Second)
			err = db.Migrate(migrateCtx)
			cancelMigrate()
			if err == nil {
				testDB = db
				os.Exit(m.Run())
			}
		}
	}
	if pgtest.Required() {
		panic("PHENK_TEST_REQUIRED is set but the test database is unusable: " + err.Error())
	}
	os.Stderr.WriteString("lifecycle: skipping database tests: " + err.Error() + "\n")
	os.Exit(m.Run())
}

type fixture struct {
	t         *testing.T
	db        *pg.DB
	blobs     *blob.FS
	blobDir   string
	keyring   *crypto.Keyring
	allocator *alloc.Allocator
	runner    *Runner

	randomDomain core.Domain
	publicDomain core.Domain

	// clock is what the runner reads, so a test can move time without waiting.
	clock time.Time
}

func newFixture(t *testing.T, configure ...func(*Options)) *fixture {
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

	f := &fixture{
		t: t, db: testDB, blobs: blobs, blobDir: blobDir, keyring: keyring,
		clock: time.Now(),
	}
	f.randomDomain = f.addDomain("rand.test", core.PoolRandom)
	f.publicDomain = f.addDomain("pub.test", core.PoolPublic)
	f.allocator = alloc.New(keyring, alloc.Options{
		DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, NamedRetention: 168 * time.Hour,
	})

	opts := Options{
		PurgeGrace:     5 * time.Minute,
		ReservePeriod:  90 * 24 * time.Hour,
		ExpiringNotice: 5 * time.Minute,
		NamedRetention: 168 * time.Hour,
	}
	for _, fn := range configure {
		fn(&opts)
	}
	f.runner = New(testDB, blobs, opts)
	f.runner.now = func() time.Time { return f.clock }
	return f
}

func (f *fixture) addDomain(name string, pool core.Pool) core.Domain {
	f.t.Helper()
	d := &core.Domain{Name: name, State: core.DomainActive, Pool: pool}
	if err := pg.CreateDomain(context.Background(), f.db, d); err != nil {
		f.t.Fatalf("CreateDomain(%s): %v", name, err)
	}
	return *d
}

// advance moves the runner's clock forward.
func (f *fixture) advance(d time.Duration) { f.clock = f.clock.Add(d) }

func (f *fixture) allocate(ttl time.Duration) *core.Identity {
	f.t.Helper()
	result, err := f.allocator.AllocateRandom(context.Background(), f.db, "session-1", ttl)
	if err != nil {
		f.t.Fatalf("AllocateRandom: %v", err)
	}
	return result.Identity
}

func (f *fixture) named(localPart string) *core.Identity {
	f.t.Helper()
	result, err := f.allocator.ResolveOrCreateNamed(context.Background(), f.db, localPart)
	if err != nil {
		f.t.Fatalf("ResolveOrCreateNamed: %v", err)
	}
	return result.Identity
}

// deliver commits a message the way the SMTP path does, receivedAt included so
// retention can be tested without waiting a week.
func (f *fixture) deliver(identity *core.Identity, body string, receivedAt time.Time) core.UUID {
	f.t.Helper()
	return f.deliverShared([]*core.Identity{identity}, body, receivedAt)[0]
}

// deliverShared commits one message to several identities in a single
// transaction, exactly as one SMTP transaction with several recipients does:
// one set of bytes, one content key, and a separate wrapping of it per
// identity. Delivering the same body twice in separate calls would produce two
// blobs, because each call mints its own content key.
func (f *fixture) deliverShared(identities []*core.Identity, body string, receivedAt time.Time) []core.UUID {
	f.t.Helper()
	ctx := context.Background()

	contentKey, rawContentKey, err := crypto.NewContentKey()
	if err != nil {
		f.t.Fatalf("NewContentKey: %v", err)
	}
	var sealed bytes.Buffer
	if _, err := contentKey.SealStream(&sealed, bytes.NewReader([]byte(body))); err != nil {
		f.t.Fatalf("SealStream: %v", err)
	}

	sha, storedSize, err := f.blobs.Put(ctx, bytes.NewReader(sealed.Bytes()))
	if err != nil {
		f.t.Fatalf("Put: %v", err)
	}

	deliveryIDs := make([]core.UUID, 0, len(identities))
	err = f.db.InTx(ctx, func(q pg.Querier) error {
		deliveryIDs = deliveryIDs[:0]
		for _, identity := range identities {
			stored, err := pg.IdentityForUpdate(ctx, q, identity.ID)
			if err != nil {
				return err
			}
			dataKey, err := f.keyring.Unwrap(stored.ID, stored.WrappedDataKey)
			if err != nil {
				return err
			}
			wrapped, err := dataKey.Seal(rawContentKey)
			dataKey.Destroy()
			if err != nil {
				return err
			}

			blobID, _, err := pg.AcquireBlob(ctx, q, sha, storedSize, f.blobs.Locate(sha))
			if err != nil {
				return err
			}
			seq, err := pg.ReserveDeliverySlot(ctx, q, stored.ID, int64(len(body)))
			if err != nil {
				return err
			}
			d := &core.Delivery{
				IdentityID:        stored.ID,
				Seq:               seq,
				BlobID:            blobID,
				EnvelopeFrom:      "sender@example.com",
				ClientIP:          mustAddr("198.51.100.7"),
				SizeBytes:         int64(len(body)),
				State:             core.DeliveryReceived,
				ReceivedAt:        receivedAt,
				WrappedContentKey: wrapped,
			}
			if err := pg.InsertDelivery(ctx, q, d); err != nil {
				return err
			}
			deliveryIDs = append(deliveryIDs, d.ID)
		}
		return nil
	})
	if err != nil {
		f.t.Fatalf("delivery commit: %v", err)
	}
	return deliveryIDs
}

func (f *fixture) countRows(table string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		f.t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

func (f *fixture) countEvents(eventType string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(context.Background(),
		`SELECT count(*) FROM events WHERE type = $1`, eventType).Scan(&n); err != nil {
		f.t.Fatalf("counting events: %v", err)
	}
	return n
}

func (f *fixture) reload(id core.UUID) *core.Identity {
	f.t.Helper()
	identity, err := pg.IdentityByID(context.Background(), f.db, id)
	if err != nil {
		f.t.Fatalf("IdentityByID: %v", err)
	}
	return identity
}

// blobFiles counts what is actually on disk.
func (f *fixture) blobFiles() int {
	f.t.Helper()
	count := 0
	_ = filepathWalk(f.blobDir, func(isDir bool) {
		if !isDir {
			count++
		}
	})
	return count
}
