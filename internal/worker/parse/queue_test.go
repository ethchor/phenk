package parse

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/worker/queue"
)

// TestEnqueueInsideTheCommitTransaction proves the property the design depends
// on: a committed delivery always has a parse job, and a rolled back one never
// does. Enqueuing after the commit instead would leave a window where a crash
// loses the job and the message sits unparsed forever.
func TestEnqueueInsideTheCommitTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := queue.Migrate(ctx, f.db); err != nil {
		t.Fatalf("queue.Migrate: %v", err)
	}
	if _, err := f.db.Exec(ctx, `DELETE FROM river_job`); err != nil {
		t.Fatalf("clearing jobs: %v", err)
	}

	inserter, err := queue.NewInserter(f.db)
	if err != nil {
		t.Fatalf("NewInserter: %v", err)
	}

	// A transaction that rolls back must leave no job behind.
	doomed := core.NewUUID()
	failure := errRollback
	_ = f.db.InTx(ctx, func(q pg.Querier) error {
		if err := Enqueue(ctx, inserter, q, doomed); err != nil {
			return err
		}
		return failure
	})
	if n := countJobs(t, f, doomed); n != 0 {
		t.Fatalf("%d jobs survived a rolled back transaction", n)
	}

	// A transaction that commits must leave exactly one.
	committed := core.NewUUID()
	if err := f.db.InTx(ctx, func(q pg.Querier) error {
		return Enqueue(ctx, inserter, q, committed)
	}); err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if n := countJobs(t, f, committed); n != 1 {
		t.Fatalf("%d jobs after a committed transaction, want 1", n)
	}
}

func TestEnqueueRefusesOutsideATransaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := queue.Migrate(ctx, f.db); err != nil {
		t.Fatalf("queue.Migrate: %v", err)
	}
	inserter, err := queue.NewInserter(f.db)
	if err != nil {
		t.Fatalf("NewInserter: %v", err)
	}
	// The pool is a Querier but not a transaction, and enqueuing against it
	// would silently give up the atomicity the design relies on.
	if err := Enqueue(ctx, inserter, f.db, core.NewUUID()); err == nil {
		t.Fatal("Enqueue succeeded outside a transaction")
	}
}

// TestWorkerParsesAQueuedDelivery runs a real River client end to end.
func TestWorkerParsesAQueuedDelivery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := queue.Migrate(ctx, f.db); err != nil {
		t.Fatalf("queue.Migrate: %v", err)
	}
	if _, err := f.db.Exec(ctx, `DELETE FROM river_job`); err != nil {
		t.Fatalf("clearing jobs: %v", err)
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, NewWorker(f.parser))
	client, err := queue.NewWorker(f.db, workers, 2)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := client.Start(runCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = client.Stop(stopCtx)
	}()

	inserter, err := queue.NewInserter(f.db)
	if err != nil {
		t.Fatalf("NewInserter: %v", err)
	}
	deliveryID := f.deliver("plain-text.eml")
	if err := f.db.InTx(ctx, func(q pg.Querier) error {
		return Enqueue(ctx, inserter, q, deliveryID)
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		delivery, err := pg.DeliveryByID(ctx, f.db, deliveryID)
		if err != nil {
			t.Fatalf("DeliveryByID: %v", err)
		}
		if delivery.State == core.DeliveryParsed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queued delivery was never parsed, state is %q", delivery.State)
		}
		time.Sleep(50 * time.Millisecond)
	}

	parsed, err := pg.ParsedMessageByDelivery(ctx, f.db, deliveryID)
	if err != nil {
		t.Fatalf("ParsedMessageByDelivery: %v", err)
	}
	if parsed.Subject != "Your verification code" {
		t.Fatalf("subject = %q", parsed.Subject)
	}
}

func countJobs(t *testing.T, f *fixture, deliveryID core.UUID) int {
	t.Helper()
	var n int
	err := f.db.QueryRow(context.Background(),
		`SELECT count(*) FROM river_job WHERE args->>'delivery_id' = $1`, deliveryID.String()).Scan(&n)
	if err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	return n
}
