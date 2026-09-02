// Package config loads Phenk's configuration from the environment.
//
// Everything is an environment variable and everything has a default that
// works for a single-host self-hoster, except the two things that cannot have
// one: the database URL and the master key.
package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/ethchor/phenk/internal/crypto"
)

// Config is the whole of Phenk's configuration.
type Config struct {
	// Env is "development" or "production". It only affects log formatting and
	// how loudly missing configuration is complained about.
	Env      string     `env:"PHENK_ENV" envDefault:"development"`
	LogLevel string     `env:"PHENK_LOG_LEVEL" envDefault:"info"`
	Database Database   `envPrefix:"PHENK_"`
	Master   MasterKey  `envPrefix:"PHENK_"`
	Blob     BlobConfig `envPrefix:"PHENK_BLOB_"`
	SMTP     SMTP       `envPrefix:"PHENK_SMTP_"`
	API      API        `envPrefix:"PHENK_API_"`
	Identity Identity   `envPrefix:"PHENK_IDENTITY_"`
}

// Database is the Postgres connection.
type Database struct {
	URL      string `env:"DATABASE_URL"`
	MaxConns int32  `env:"DATABASE_MAX_CONNS" envDefault:"16"`
	// Migrate applies pending migrations at startup. Convenient for a
	// single-host deployment, and something a fleet operator turns off in
	// favour of a deliberate migration step.
	Migrate bool `env:"DATABASE_MIGRATE" envDefault:"true"`
}

// MasterKey is the base64 master key every identity data key is wrapped under.
type MasterKey struct {
	Key string `env:"MASTER_KEY"`
}

// BlobConfig selects and configures blob storage. Only the filesystem backend
// exists in v0.
type BlobConfig struct {
	Backend string `env:"BACKEND" envDefault:"fs"`
	Dir     string `env:"DIR" envDefault:"./data/blobs"`
}

// SMTP configures the ingestion listener.
type SMTP struct {
	Addr     string `env:"ADDR" envDefault:":25"`
	Hostname string `env:"HOSTNAME" envDefault:"localhost"`

	// MaxMessageBytes is the hard cap on a single message. DATA aborts the
	// moment it is exceeded rather than buffering and rejecting at the end.
	MaxMessageBytes int64 `env:"MAX_MESSAGE_BYTES" envDefault:"26214400"`
	// MaxPublicMessageBytes is the lower cap for public-pool domains, per the
	// abuse controls in §6.5.
	MaxPublicMessageBytes int64 `env:"MAX_PUBLIC_MESSAGE_BYTES" envDefault:"10485760"`

	MaxRecipients        int           `env:"MAX_RECIPIENTS" envDefault:"10"`
	MaxConnectionsPerIP  int           `env:"MAX_CONNECTIONS_PER_IP" envDefault:"10"`
	IdleTimeout          time.Duration `env:"IDLE_TIMEOUT" envDefault:"30s"`
	TLSCertFile          string        `env:"TLS_CERT_FILE"`
	TLSKeyFile           string        `env:"TLS_KEY_FILE"`
	ProvisionsPerIPHour  int           `env:"PROVISIONS_PER_IP_HOUR" envDefault:"20"`
	GlobalProvisionsHour int           `env:"GLOBAL_PROVISIONS_HOUR" envDefault:"5000"`
}

// API configures the HTTP tier.
type API struct {
	Addr           string        `env:"ADDR" envDefault:":8080"`
	PublicURL      string        `env:"PUBLIC_URL" envDefault:"http://localhost:8080"`
	MaxWaitTimeout time.Duration `env:"MAX_WAIT_TIMEOUT" envDefault:"120s"`
	CookieName     string        `env:"COOKIE_NAME" envDefault:"phenk_session"`
	CookieSecure   bool          `env:"COOKIE_SECURE" envDefault:"true"`
}

// Identity configures address lifecycle defaults.
type Identity struct {
	DefaultTTL     time.Duration `env:"DEFAULT_TTL" envDefault:"1h"`
	MaxTTL         time.Duration `env:"MAX_TTL" envDefault:"24h"`
	PurgeGrace     time.Duration `env:"PURGE_GRACE" envDefault:"5m"`
	ReservePeriod  time.Duration `env:"RESERVE_PERIOD" envDefault:"2160h"` // 90 days
	ExpiringNotice time.Duration `env:"EXPIRING_NOTICE" envDefault:"5m"`
	NamedRetention time.Duration `env:"NAMED_RETENTION" envDefault:"168h"`
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate rejects a configuration that would fail later in a more confusing
// place. The two required values have no safe default: a database URL, and a
// master key without which no identity can be created at all.
func (c *Config) Validate() error {
	var problems []error

	if c.Database.URL == "" {
		problems = append(problems, errors.New("PHENK_DATABASE_URL is required"))
	}
	if c.Master.Key == "" {
		problems = append(problems, errors.New("PHENK_MASTER_KEY is required (generate one with `phenk genkey`)"))
	} else if _, err := crypto.ParseMasterKey(c.Master.Key); err != nil {
		problems = append(problems, fmt.Errorf("PHENK_MASTER_KEY: %w", err))
	}
	if c.Blob.Backend != "fs" {
		problems = append(problems, fmt.Errorf("PHENK_BLOB_BACKEND=%q: only \"fs\" exists in v0", c.Blob.Backend))
	}
	if c.Blob.Dir == "" {
		problems = append(problems, errors.New("PHENK_BLOB_DIR is required"))
	}
	if c.SMTP.MaxPublicMessageBytes > c.SMTP.MaxMessageBytes {
		problems = append(problems, errors.New("PHENK_SMTP_MAX_PUBLIC_MESSAGE_BYTES must not exceed PHENK_SMTP_MAX_MESSAGE_BYTES"))
	}
	if c.Identity.DefaultTTL > c.Identity.MaxTTL {
		problems = append(problems, errors.New("PHENK_IDENTITY_DEFAULT_TTL must not exceed PHENK_IDENTITY_MAX_TTL"))
	}
	if (c.SMTP.TLSCertFile == "") != (c.SMTP.TLSKeyFile == "") {
		problems = append(problems, errors.New("PHENK_SMTP_TLS_CERT_FILE and PHENK_SMTP_TLS_KEY_FILE must be set together"))
	}

	return errors.Join(problems...)
}

// MasterKeyBytes decodes the configured master key.
func (c *Config) MasterKeyBytes() ([]byte, error) { return crypto.ParseMasterKey(c.Master.Key) }
