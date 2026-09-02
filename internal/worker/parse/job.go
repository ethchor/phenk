package parse

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// JobArgs is the queued request to parse one delivery.
type JobArgs struct {
	DeliveryID core.UUID `json:"delivery_id"`
}

// Kind implements river.JobArgs.
func (JobArgs) Kind() string { return "phenk.parse" }

// InsertOpts caps retries at MaxAttempts. A message that has failed three times
// is not going to succeed on the fourth, and the raw message stays readable
// whether or not it is ever parsed.
func (JobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: MaxAttempts}
}

// Worker runs parse jobs.
type Worker struct {
	river.WorkerDefaults[JobArgs]
	parser *Parser
}

// NewWorker wraps a Parser as a queue worker.
func NewWorker(parser *Parser) *Worker { return &Worker{parser: parser} }

// Work implements river.Worker.
func (w *Worker) Work(ctx context.Context, job *river.Job[JobArgs]) error {
	err := w.parser.Parse(ctx, job.Args.DeliveryID)
	if errors.Is(err, ErrPermanent) {
		// Retrying will not help, so stop rather than burning the remaining
		// attempts and the delay between them on a message that cannot parse.
		return river.JobCancel(err)
	}
	return err
}

// Enqueue schedules a parse inside the transaction that commits the delivery,
// so a committed message always has a job and a rolled back one never does.
//
// Enqueuing after the commit instead would leave a window where a crash loses
// the job and the message sits unparsed forever, with nothing to notice it.
func Enqueue(ctx context.Context, client *river.Client[pgx.Tx], q pg.Querier, deliveryID core.UUID) error {
	tx, ok := q.(pgx.Tx)
	if !ok {
		return errors.New("parse: enqueue must run inside a transaction")
	}
	_, err := client.InsertTx(ctx, tx, JobArgs{DeliveryID: deliveryID}, nil)
	return err
}
