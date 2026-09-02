package pg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/ethchor/phenk/migrations"
)

// migrationLockID is an arbitrary but fixed advisory lock key. Two binaries
// starting at once must not both try to create the schema.
const migrationLockID int64 = 0x7068656e6b0001

// Migrate applies every embedded migration that has not been applied yet, in
// lexical order, each in its own transaction. It is safe to run on every boot
// and safe to run concurrently from several processes.
func (db *DB) Migrate(ctx context.Context) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("pg: acquiring connection for migration: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("pg: taking migration lock: %w", err)
	}
	defer func() {
		// Best effort: releasing the session lock on a broken connection is
		// moot, since the lock dies with the session.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
		  name        text PRIMARY KEY,
		  checksum    text NOT NULL,
		  applied_at  timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("pg: creating schema_migrations: %w", err)
	}

	applied := map[string]string{}
	rows, err := conn.Query(ctx, `SELECT name, checksum FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("pg: reading schema_migrations: %w", err)
	}
	for rows.Next() {
		var name, sum string
		if err := rows.Scan(&name, &sum); err != nil {
			rows.Close()
			return fmt.Errorf("pg: reading schema_migrations: %w", err)
		}
		applied[name] = sum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pg: reading schema_migrations: %w", err)
	}

	for _, name := range files {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("pg: reading migration %s: %w", name, err)
		}
		sum := checksum(body)

		if prev, ok := applied[name]; ok {
			if prev != sum {
				// An applied migration that has since been edited means the
				// schema on disk and the schema in the database have diverged.
				// Refusing to start is the only safe response.
				return fmt.Errorf("pg: migration %s was modified after it was applied (have %s, want %s)", name, sum, prev)
			}
			continue
		}

		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)`, name, sum)
			return err
		}); err != nil {
			return fmt.Errorf("pg: applying migration %s: %w", name, err)
		}
		slog.Info("applied migration", "name", name)
	}
	return nil
}

func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("pg: listing migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("pg: no migrations embedded")
	}
	return names, nil
}

func checksum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
