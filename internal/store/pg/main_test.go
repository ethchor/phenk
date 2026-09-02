package pg

import (
	"context"
	"os"
	"testing"
	"time"
)

// defaultTestDSN points at the database docker-compose brings up, so a
// developer who has run `make dev-db` can run the storage tests with no extra
// setup. Override it with PHENK_TEST_DSN.
const defaultTestDSN = "postgres://phenk:phenk@127.0.0.1:5432/phenk_test?sslmode=disable"

var testDB *DB

func TestMain(m *testing.M) {
	dsn := os.Getenv("PHENK_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := Open(ctx, dsn, 8)
	cancel()
	if err != nil {
		// A developer without Postgres running still gets a green `go test
		// ./...`; CI sets PHENK_TEST_REQUIRED so a missing database is a
		// failure there rather than a silent gap in coverage.
		if os.Getenv("PHENK_TEST_REQUIRED") != "" {
			panic("PHENK_TEST_REQUIRED is set but the test database is unreachable: " + err.Error())
		}
		os.Stderr.WriteString("pg: skipping storage tests, no database at " + dsn + "\n")
		os.Exit(0)
	}
	defer db.Close()

	migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 60*time.Second)
	if err := db.Migrate(migrateCtx); err != nil {
		cancelMigrate()
		panic("pg: migrating test database: " + err.Error())
	}
	cancelMigrate()

	testDB = db
	os.Exit(m.Run())
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
