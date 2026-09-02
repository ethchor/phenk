package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ethchor/phenk/internal/api"
	"github.com/ethchor/phenk/internal/events"
)

// runAPI serves the HTTP tier until the context is cancelled.
func (r *runtime) runAPI(ctx context.Context) error {
	hub := events.NewHub(r.db)
	go func() { _ = hub.Run(ctx) }()

	server := api.New(api.Config{
		PublicURL:      r.cfg.API.PublicURL,
		CookieName:     r.cfg.API.CookieName,
		CookieSecure:   r.cfg.API.CookieSecure,
		MaxWaitTimeout: r.cfg.API.MaxWaitTimeout,
		DefaultTTL:     r.cfg.Identity.DefaultTTL,
		MaxTTL:         r.cfg.Identity.MaxTTL,
		NamedPerIPHour: r.cfg.SMTP.ProvisionsPerIPHour,
		Version:        version,
	}, r.db, r.blobs, r.keyring, r.allocator, hub)

	httpServer := &http.Server{
		Addr:    r.cfg.API.Addr,
		Handler: server.Handler(),
		// No write timeout: long polls and event streams hold a response open
		// on purpose, and a blanket deadline would cut them off mid-wait. The
		// read and idle timeouts still bound a client that connects and says
		// nothing.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errs := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", r.cfg.API.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
		close(errs)
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("api: shutting down: %w", err)
	}
	return nil
}
