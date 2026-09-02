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

// Migrate brings River's own tables up to date. River owns its schema, so this
// is separate from Phenk's migrations rather than copied into them.
func Migrate(ctx context.Context, db *pg.DB) error {
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
