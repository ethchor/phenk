package parse

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/sanitize"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/testsupport/pgtest"
)

var testDB *pg.DB

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "parse")
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
	os.Stderr.WriteString("parse: skipping database tests: " + err.Error() + "\n")
	os.Exit(m.Run())
}

// fixture is one delivery of a golden message, ready to parse.
type fixture struct {
	t        *testing.T
	db       *pg.DB
	blobs    *blob.FS
	keyring  *crypto.Keyring
	parser   *Parser
	identity *core.Identity
	dataKey  *crypto.DataKey
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
	blobs, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	domain := &core.Domain{Name: "rand.test", State: core.DomainActive, Pool: core.PoolRandom}
	if err := pg.CreateDomain(ctx, testDB, domain); err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	allocator := alloc.New(keyring, alloc.Options{DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour})
	result, err := allocator.AllocateRandom(ctx, testDB, "session-1", time.Hour)
	if err != nil {
		t.Fatalf("AllocateRandom: %v", err)
	}
	dataKey, err := keyring.Unwrap(result.Identity.ID, result.Identity.WrappedDataKey)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}

	opts := Options{}
	for _, fn := range configure {
		fn(&opts)
	}

	return &fixture{
		t: t, db: testDB, blobs: blobs, keyring: keyring,
		parser:   New(testDB, blobs, keyring, sanitize.New([]byte("test-image-proxy-signing-key-32b")), opts),
		identity: result.Identity,
		dataKey:  dataKey,
	}
}

// deliver stores a golden message as a committed delivery, the way the SMTP
// path would have.
func (f *fixture) deliver(name string) core.UUID {
	f.t.Helper()
	ctx := context.Background()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "mime", name))
	if err != nil {
		f.t.Fatalf("reading fixture: %v", err)
	}
	return f.deliverRaw(ctx, raw)
}

func (f *fixture) deliverRaw(ctx context.Context, raw []byte) core.UUID {
	f.t.Helper()

	sha, size, err := f.blobs.Put(ctx, bytesReader(raw))
	if err != nil {
		f.t.Fatalf("Put: %v", err)
	}

	var deliveryID core.UUID
	err = f.db.InTx(ctx, func(q pg.Querier) error {
		blobID, _, err := pg.AcquireBlob(ctx, q, sha, size, f.blobs.Locate(sha))
		if err != nil {
			return err
		}
		seq, err := pg.ReserveDeliverySlot(ctx, q, f.identity.ID, size)
		if err != nil {
			return err
		}
		d := &core.Delivery{
			IdentityID:   f.identity.ID,
			Seq:          seq,
			BlobID:       blobID,
			EnvelopeFrom: "sender@example.com",
			ClientIP:     mustAddr("198.51.100.7"),
			HELO:         "mail.example.com",
			SizeBytes:    size,
			State:        core.DeliveryReceived,
		}
		if err := pg.InsertDelivery(ctx, q, d); err != nil {
			return err
		}
		deliveryID = d.ID
		return nil
	})
	if err != nil {
		f.t.Fatalf("delivery commit: %v", err)
	}
	return deliveryID
}

// decrypt reads a stored body back the way the API will.
func (f *fixture) decrypt(sealed []byte) string {
	f.t.Helper()
	if len(sealed) == 0 {
		return ""
	}
	plain, err := f.dataKey.Open(sealed)
	if err != nil {
		f.t.Fatalf("Open: %v", err)
	}
	return string(plain)
}
