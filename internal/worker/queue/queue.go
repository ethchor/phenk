// Package queue wires River, the Postgres-backed job queue.
//
// There is no broker and no Redis: jobs live in the same database as the data
// they act on, which is what lets a parse job be enqueued inside the very
// transaction that commits the delivery it parses.
package queue

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/ethchor/phenk/internal/store/pg"
)

// Client is a River client bound to the pgx pool.
type Client = river.Client[pgx.Tx]

// migrationLockID is an arbitrary but fixed advisory lock key, distinct from
// the one Phenk's own migrations use.
const migrationLockID int64 = 0x7068656e6b0002

// Migrate brings River's own tables up to date. River owns its schema, so this
// is separate from Phenk's migrations rather than copied into them.
//
// The advisory lock is what makes it safe to call from several places at once.
// `phenk all` starts the listener and the worker together and both need the
// queue, and without the lock they race: one creates River's tables and the
// other fails on the table it finds already there, taking the whole process
// down. River's migrator does not serialise itself.
func Migrate(ctx context.Context, db *pg.DB) error {
	conn, err := db.Pool().Acquire(ctx)
	if err != nil {
		return fmt.Errorf("queue: acquiring connection for migration: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("queue: taking migration lock: %w", err)
	}
	defer func() {
		// The lock dies with the session anyway, so a failure to release it on
		// a broken connection is moot.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	migrator, err := rivermigrate.New(riverpgxv5.New(db.Pool()), nil)
	if err != nil {
		return fmt.Errorf("queue: building migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("queue: migrating: %w", err)
	}
	return nil
}

// NewInserter builds a client that only enqueues. The SMTP process needs to
// insert parse jobs but must never spend its own capacity running them.
func NewInserter(db *pg.DB) (*Client, error) {
	client, err := river.NewClient(riverpgxv5.New(db.Pool()), &river.Config{
		Logger: slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("queue: building inserter: %w", err)
	}
	return client, nil
}

// NewWorker builds a client that runs jobs from the default queue, plus any
// periodic jobs it is given.
func NewWorker(db *pg.DB, workers *river.Workers, maxWorkers int, periodic ...*river.PeriodicJob) (*Client, error) {
	if maxWorkers <= 0 {
		maxWorkers = 4
	}
	client, err := river.NewClient(riverpgxv5.New(db.Pool()), &river.Config{
		Logger:  slog.Default(),
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: maxWorkers},
		},
		// River elects one leader across every worker process, and only the
		// leader inserts periodic jobs. Running several workers therefore does
		// not run the lifecycle jobs several times over.
		PeriodicJobs: periodic,
	})
	if err != nil {
		return nil, fmt.Errorf("queue: building worker: %w", err)
	}
	return client, nil
}
