package main

import (
	"context"
	"log/slog"
	"sync"
)

// runAll runs every mode in one process.
//
// This is the self-hoster's deployment: one binary, one database, one machine.
// A fleet operator runs the modes separately so that a burst of inbound mail
// cannot starve the API, but for a single host the simplicity is worth more
// than the isolation.
func (r *runtime) runAll(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	modes := map[string]func(context.Context) error{
		"smtpd":  r.runSMTPD,
		"api":    r.runAPI,
		"worker": r.runWorker,
	}

	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for name, run := range modes {
		wg.Add(1)
		go func(name string, run func(context.Context) error) {
			defer wg.Done()
			if err := run(ctx); err != nil {
				slog.Error("run mode failed", "mode", name, "error", err)
				// One mode failing takes the process down rather than leaving
				// a half-running service: a listener with no worker accepts
				// mail nobody will ever parse.
				once.Do(func() { first = err })
				cancel()
			}
		}(name, run)
	}
	wg.Wait()
	return first
}
