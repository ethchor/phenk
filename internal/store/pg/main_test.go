package pg

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/testsupport/pgtest"
)

var testDB *DB

func TestMain(m *testing.M) {
	// Each test package gets its own database. `go test ./...` runs packages
	// in parallel and every storage test starts by truncating the tables it
	// uses, so one shared database means one package deleting another
	// package's fixtures halfway through a test.
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "store")
	cancelSetup()
	if err == nil {
		var db *DB
		openCtx, cancelOpen := context.WithTimeout(context.Background(), 15*time.Second)
		db, err = Open(openCtx, dsn, 8)
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

	// A developer without Postgres running still gets a green `go test ./...`.
	// CI sets PHENK_TEST_REQUIRED so a missing database is a failure there
	// rather than a silent gap in coverage.
	if pgtest.Required() {
		panic("PHENK_TEST_REQUIRED is set but the test database is unusable: " + err.Error())
	}
	os.Stderr.WriteString("pg: skipping storage tests: " + err.Error() + "\n")
	os.Exit(0)
}

// reset empties every table a test may write to. blocked_local_parts is left
// alone: its contents are seeded by a migration and are part of the schema, not
// test fixtures.
func reset(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec(context.Background(), `
		TRUNCATE attachments, parsed_messages, deliveries, events, identities, blobs, domains RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
}
