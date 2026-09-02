package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/ethchor/phenk/internal/sanitize"
	"github.com/ethchor/phenk/internal/worker/lifecycle"
	"github.com/ethchor/phenk/internal/worker/parse"
	"github.com/ethchor/phenk/internal/worker/queue"
)

// runWorker runs queued jobs until the context is cancelled.
func (r *runtime) runWorker(ctx context.Context) error {
	if err := queue.Migrate(ctx, r.db); err != nil {
		return err
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, parse.NewWorker(r.parser()))
	lifecycle.Register(workers, r.lifecycle())

	client, err := queue.NewWorker(r.db, workers, r.cfg.Worker.MaxWorkers,
		lifecycle.PeriodicJobs()...)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("worker: starting: %w", err)
	}
	slog.Info("worker running", "max_workers", r.cfg.Worker.MaxWorkers)

	<-ctx.Done()

	// Stop with a fresh context: the one that just ended is what told us to
	// stop, and shutdown still needs to finish the jobs already in flight.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.cfg.Worker.ShutdownTimeout())
	defer cancel()
	if err := client.Stop(stopCtx); err != nil {
		return fmt.Errorf("worker: stopping: %w", err)
	}
	return nil
}

// lifecycle builds the runner behind the expiry, purge and retention jobs.
func (r *runtime) lifecycle() *lifecycle.Runner {
	return lifecycle.New(r.db, r.blobs, lifecycle.Options{
		PurgeGrace:     r.cfg.Identity.PurgeGrace,
		ReservePeriod:  r.cfg.Identity.ReservePeriod,
		ExpiringNotice: r.cfg.Identity.ExpiringNotice,
		NamedRetention: r.cfg.Identity.NamedRetention,
	})
}

// parser builds the message parser.
func (r *runtime) parser() *parse.Parser {
	// The image proxy signing key is derived from the master key, so an
	// operator has one secret to manage rather than two.
	return parse.New(r.db, r.blobs, r.keyring,
		sanitize.New(r.keyring.Derive("image-proxy")),
		parse.Options{MaxAttachmentBytes: r.cfg.Worker.MaxAttachmentBytes})
}
