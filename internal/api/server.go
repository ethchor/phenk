// Package api serves Phenk's HTTP surface.
//
// The handler signatures come from api/openapi.yaml through oapi-codegen, so a
// handler that drifts from the published contract stops compiling. Handler
// bodies are written by hand; only the interface and the types are generated.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/events"
	"github.com/ethchor/phenk/internal/sanitize"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
)

// Config configures the HTTP tier.
type Config struct {
	PublicURL      string
	CookieName     string
	CookieSecure   bool
	MaxWaitTimeout time.Duration

	// DefaultTTL and MaxTTL bound how long a random address may live.
	DefaultTTL time.Duration
	MaxTTL     time.Duration

	// NamedPerIPHour bounds how fast one caller can mint public inboxes,
	// matching the limit the SMTP path applies to the same operation.
	NamedPerIPHour int

	// Version is reported by the health endpoint.
	Version string
}

func (c *Config) withDefaults() {
	if c.CookieName == "" {
		c.CookieName = "phenk_session"
	}
	if c.MaxWaitTimeout <= 0 {
		c.MaxWaitTimeout = 120 * time.Second
	}
	if c.DefaultTTL <= 0 {
		c.DefaultTTL = time.Hour
	}
	if c.MaxTTL <= 0 {
		c.MaxTTL = 24 * time.Hour
	}
	if c.NamedPerIPHour <= 0 {
		c.NamedPerIPHour = 20
	}
}

// Server implements apigen.ServerInterface.
type Server struct {
	cfg       Config
	db        *pg.DB
	blobs     blob.Store
	keyring   *crypto.Keyring
	allocator *alloc.Allocator
	hub       *events.Hub
	sanitizer *sanitize.Sanitizer
	images    *imageProxy
	namedRate *rateLimiter
}

var _ apigen.ServerInterface = (*Server)(nil)

// New builds the HTTP server.
func New(cfg Config, db *pg.DB, blobs blob.Store, keyring *crypto.Keyring, allocator *alloc.Allocator, hub *events.Hub) *Server {
	cfg.withDefaults()
	sanitizer := sanitize.New(keyring.Derive("image-proxy"))
	return &Server{
		cfg:       cfg,
		db:        db,
		blobs:     blobs,
		keyring:   keyring,
		allocator: allocator,
		hub:       hub,
		sanitizer: sanitizer,
		images:    newImageProxy(sanitizer),
		namedRate: newRateLimiter(cfg.NamedPerIPHour, time.Hour),
	}
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	// Streaming and long-polling handlers manage their own deadlines, so a
	// blanket timeout here would cut them off mid-wait.
	router.Use(requestLogger)

	return apigen.HandlerFromMux(s, router)
}

// Health implements apigen.ServerInterface.
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	code := http.StatusOK
	if err := s.db.Ping(r.Context()); err != nil {
		status, code = "degraded", http.StatusServiceUnavailable
	}
	version := s.cfg.Version
	writeJSON(w, code, apigen.Health{Status: apigen.HealthStatus(status), Version: &version})
}
