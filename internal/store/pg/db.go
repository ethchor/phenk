// Package pg is Phenk's PostgreSQL storage layer.
//
// Every query in this package is written as a package-level function taking a
// Querier, so the same code runs against the pool or inside a transaction. The
// SMTP commit path needs that: blob row, delivery row and event row have to
// land in one transaction, because acknowledging a message we could still lose
// would break invariant 1.
package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("pg: not found")

// ErrConflict is returned when a unique constraint rejects a write, most often
// because an address is already allocated.
var ErrConflict = errors.New("pg: conflict")

// Querier is the subset of pgx shared by the pool and a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB is a connection pool plus the operations Phenk needs from it.
type DB struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres and verifies the connection.
func Open(ctx context.Context, dsn string, maxConns int32) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("pg: parsing dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pg: connecting: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close releases every pooled connection.
func (db *DB) Close() { db.pool.Close() }

// Pool exposes the underlying pool for the few callers that need a dedicated
// connection, such as the LISTEN/NOTIFY hub.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Ping reports whether the database is reachable.
func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

// Exec implements Querier.
func (db *DB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.pool.Exec(ctx, sql, args...)
}

// Query implements Querier.
func (db *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.pool.Query(ctx, sql, args...)
}

// QueryRow implements Querier.
func (db *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return db.pool.QueryRow(ctx, sql, args...)
}

// InTx runs fn inside a transaction, committing if it returns nil and rolling
// back otherwise.
func (db *DB) InTx(ctx context.Context, fn func(Querier) error) error {
	return pgx.BeginFunc(ctx, db.pool, func(tx pgx.Tx) error { return fn(tx) })
}

// mapError translates the Postgres errors callers act on into package errors,
// and leaves everything else alone.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	}
	return err
}
