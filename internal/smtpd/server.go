// Package smtpd is Phenk's inbound SMTP listener.
//
// Two rules shape everything here. A recipient that cannot receive is rejected
// at RCPT TO rather than accepted and dropped, and a message is never
// acknowledged with 250 until it is durably committed. Both are hard invariants
// of the v0 plan, and both are the difference between a temporary mailbox and a
// black hole.
package smtpd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-smtp"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
)

// Config configures the listener.
type Config struct {
	Addr     string
	Hostname string

	MaxMessageBytes       int64
	MaxPublicMessageBytes int64
	MaxRecipients         int
	MaxConnectionsPerIP   int
	IdleTimeout           time.Duration

	// SpoolDir is where message bodies are streamed while they arrive. Empty
	// means the system temporary directory.
	SpoolDir string

	// ResolveCacheTTL is how long a resolved domain or accepting identity may
	// be reused without asking Postgres again. It is deliberately short: the
	// commit path re-reads under a row lock, so this only has to be short
	// enough that a burst of mail to one address does not become a burst of
	// queries.
	ResolveCacheTTL time.Duration

	// ProvisionsPerIPHour and GlobalProvisionsHour bound how fast named
	// inboxes can be created, per source and in total.
	ProvisionsPerIPHour  int
	GlobalProvisionsHour int

	TLSConfig *tls.Config

	// Enqueue schedules follow-up work inside the delivery commit
	// transaction. A nil hook means committed messages simply wait, which is
	// what a listener running without a worker should do.
	Enqueue EnqueueFunc
}

func (c *Config) withDefaults() {
	if c.MaxMessageBytes <= 0 {
		c.MaxMessageBytes = 25 << 20
	}
	if c.MaxPublicMessageBytes <= 0 || c.MaxPublicMessageBytes > c.MaxMessageBytes {
		c.MaxPublicMessageBytes = c.MaxMessageBytes
	}
	if c.MaxRecipients <= 0 {
		c.MaxRecipients = 10
	}
	if c.MaxConnectionsPerIP <= 0 {
		c.MaxConnectionsPerIP = 10
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 30 * time.Second
	}
	if c.ResolveCacheTTL <= 0 {
		c.ResolveCacheTTL = 2 * time.Second
	}
	if c.ProvisionsPerIPHour <= 0 {
		c.ProvisionsPerIPHour = 20
	}
	if c.GlobalProvisionsHour <= 0 {
		c.GlobalProvisionsHour = 5000
	}
	if c.Hostname == "" {
		c.Hostname = "localhost"
	}
}

// Server is the SMTP listener.
type Server struct {
	cfg      Config
	receiver MailReceiver

	connections     *connectionLimiter
	provisionPerIP  *rateLimiter
	provisionGlobal *rateLimiter

	// baseCtx is the context every session inherits, so shutting the server
	// down cancels the database work an in-flight session is doing rather than
	// leaving it to finish against a closing pool.
	baseCtx context.Context

	smtp *smtp.Server
}

// New builds a listener over the real storage layer.
func New(cfg Config, db *pg.DB, blobs blob.Store, allocator *alloc.Allocator, keyring *crypto.Keyring) *Server {
	cfg.withDefaults()
	return newWithReceiver(cfg, newStoreReceiver(db, blobs, allocator, keyring, cfg.Enqueue, cfg.ResolveCacheTTL))
}

// newWithReceiver builds a listener over any MailReceiver. Tests use it to
// exercise the protocol rules without a database.
func newWithReceiver(cfg Config, receiver MailReceiver) *Server {
	cfg.withDefaults()
	s := &Server{
		cfg:             cfg,
		receiver:        receiver,
		connections:     newConnectionLimiter(cfg.MaxConnectionsPerIP),
		provisionPerIP:  newRateLimiter(cfg.ProvisionsPerIPHour, time.Hour),
		provisionGlobal: newRateLimiter(cfg.GlobalProvisionsHour, time.Hour),
	}

	server := smtp.NewServer(&backend{server: s})
	server.Addr = cfg.Addr
	server.Domain = cfg.Hostname
	server.ReadTimeout = cfg.IdleTimeout
	server.WriteTimeout = cfg.IdleTimeout
	server.MaxMessageBytes = cfg.MaxMessageBytes
	server.MaxRecipients = cfg.MaxRecipients
	// Nothing authenticates to an inbound MX, and offering AUTH over a
	// cleartext connection would only invite credential stuffing.
	server.AllowInsecureAuth = false
	server.EnableSMTPUTF8 = true
	if cfg.TLSConfig != nil {
		// Opportunistic STARTTLS: senders that support it get transport
		// encryption, and the ones that do not still deliver, because an MX
		// that refuses cleartext refuses a large slice of real mail.
		server.TLSConfig = cfg.TLSConfig
	}
	s.smtp = server
	return s
}

// ListenAndServe serves until the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("smtpd: listening on %s: %w", s.cfg.Addr, err)
	}
	return s.Serve(ctx, listener)
}

// Serve serves an existing listener until the context is cancelled.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	s.baseCtx = ctx
	listener = &limitedListener{Listener: listener, limiter: s.connections}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.smtp.Close()
		case <-done:
		}
	}()
	defer close(done)

	slog.Info("smtp listening", "addr", listener.Addr().String(), "hostname", s.cfg.Hostname,
		"starttls", s.cfg.TLSConfig != nil)
	err := s.smtp.Serve(listener)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Close stops the listener.
func (s *Server) Close() error { return s.smtp.Close() }

// backend hands out sessions and enforces the per-source connection cap.
type backend struct{ server *Server }

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	// The per-source connection cap is applied by limitedListener at accept
	// time, before a client can occupy a slot by staying silent.
	ip := remoteAddr(c.Conn())

	ctx := b.server.baseCtx
	if ctx == nil {
		ctx = context.Background()
	}
	return &session{server: b.server, conn: c, ctx: ctx, clientIP: ip}, nil
}
