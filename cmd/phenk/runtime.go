package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/config"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/smtpd"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/worker/parse"
	"github.com/ethchor/phenk/internal/worker/queue"
)

// runtime is the set of dependencies every run mode shares.
type runtime struct {
	cfg       *config.Config
	db        *pg.DB
	blobs     blob.Store
	keyring   *crypto.Keyring
	allocator *alloc.Allocator
}

func newRuntime(cfg *config.Config, db *pg.DB) (*runtime, error) {
	masterKey, err := cfg.MasterKeyBytes()
	if err != nil {
		return nil, err
	}
	keyring, err := crypto.NewKeyring(masterKey)
	if err != nil {
		return nil, err
	}

	blobs, err := blob.NewFS(cfg.Blob.Dir)
	if err != nil {
		return nil, err
	}

	return &runtime{
		cfg:     cfg,
		db:      db,
		blobs:   blobs,
		keyring: keyring,
		allocator: alloc.New(keyring, alloc.Options{
			DefaultTTL:     cfg.Identity.DefaultTTL,
			MaxTTL:         cfg.Identity.MaxTTL,
			NamedRetention: cfg.Identity.NamedRetention,
		}),
	}, nil
}

// runSMTPD serves inbound mail until the context is cancelled.
func (r *runtime) runSMTPD(ctx context.Context) error {
	tlsConfig, err := loadTLS(r.cfg)
	if err != nil {
		return err
	}

	// The listener enqueues parse jobs but never runs them: a process
	// accepting mail must not spend its capacity on parsing it.
	if err := queue.Migrate(ctx, r.db); err != nil {
		return err
	}
	inserter, err := queue.NewInserter(r.db)
	if err != nil {
		return err
	}

	server := smtpd.New(smtpd.Config{
		Addr:                  r.cfg.SMTP.Addr,
		Hostname:              r.cfg.SMTP.Hostname,
		MaxMessageBytes:       r.cfg.SMTP.MaxMessageBytes,
		MaxPublicMessageBytes: r.cfg.SMTP.MaxPublicMessageBytes,
		MaxRecipients:         r.cfg.SMTP.MaxRecipients,
		MaxConnectionsPerIP:   r.cfg.SMTP.MaxConnectionsPerIP,
		IdleTimeout:           r.cfg.SMTP.IdleTimeout,
		ResolveCacheTTL:       2 * time.Second,
		ProvisionsPerIPHour:   r.cfg.SMTP.ProvisionsPerIPHour,
		GlobalProvisionsHour:  r.cfg.SMTP.GlobalProvisionsHour,
		TLSConfig:             tlsConfig,
		Enqueue: func(ctx context.Context, q pg.Querier, deliveryID core.UUID) error {
			return parse.Enqueue(ctx, inserter, q, deliveryID)
		},
	}, r.db, r.blobs, r.allocator)

	return server.ListenAndServe(ctx)
}

// loadTLS reads the certificate the SMTP listener offers for STARTTLS. Config
// validation has already established that the two file paths are set together
// or not at all.
func loadTLS(cfg *config.Config) (*tls.Config, error) {
	if cfg.SMTP.TLSCertFile == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.SMTP.TLSCertFile, cfg.SMTP.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("loading smtp tls certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
