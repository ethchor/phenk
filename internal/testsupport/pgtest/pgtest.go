// Package pgtest gives each test package its own Postgres database.
//
// `go test ./...` runs packages in parallel, and every storage test starts by
// truncating the tables it works with. Sharing one database across packages
// means one package's reset deletes another package's fixtures halfway through
// a test, which produces failures that look like real bugs and move around
// between runs.
//
// It talks to pgx directly rather than through internal/store/pg, so the
// storage package's own tests can use it without an import cycle.
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DefaultDSN points at the database docker-compose brings up, so the storage
// tests run with no extra setup. Override it with PHENK_TEST_DSN.
const DefaultDSN = "postgres://phenk:phenk@127.0.0.1:5432/phenk_test?sslmode=disable"

// BaseDSN returns the configured base connection string.
func BaseDSN() string {
	if dsn := os.Getenv("PHENK_TEST_DSN"); dsn != "" {
		return dsn
	}
	return DefaultDSN
}

// Required reports whether a missing database should fail the run rather than
// skip it. CI sets PHENK_TEST_REQUIRED so an unreachable database is a failure
// there instead of a silent gap in coverage.
func Required() bool { return os.Getenv("PHENK_TEST_REQUIRED") != "" }

// DatabaseFor creates a database named after the calling test package, if it
// does not already exist, and returns a DSN pointing at it.
func DatabaseFor(ctx context.Context, suffix string) (string, error) {
	base := BaseDSN()

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("pgtest: parsing PHENK_TEST_DSN: %w", err)
	}
	baseName := strings.TrimPrefix(parsed.Path, "/")
	if baseName == "" {
		return "", errors.New("pgtest: the test dsn names no database")
	}
	name := sanitize(baseName + "_" + suffix)

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", err
	}
	defer admin.Close(ctx)

	// Postgres has no CREATE DATABASE IF NOT EXISTS, and two packages starting
	// at the same moment will race here, so an already-exists error is the
	// expected outcome rather than a failure.
	_, err = admin.Exec(ctx, `CREATE DATABASE "`+name+`"`)
	if err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "42P04" && pgErr.Code != "23505") {
			return "", fmt.Errorf("pgtest: creating %s: %w", name, err)
		}
	}

	parsed.Path = "/" + name
	return parsed.String(), nil
}

// sanitize keeps a database name to characters that need no quoting rules
// beyond the ones used above.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if len(name) > 63 { // Postgres identifier limit
		name = name[:63]
	}
	return name
}
