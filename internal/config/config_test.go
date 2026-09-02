package config

import (
	"strings"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/crypto"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("PHENK_DATABASE_URL", "postgres://phenk:phenk@127.0.0.1:5432/phenk")
	t.Setenv("PHENK_MASTER_KEY", crypto.GenerateMasterKey())
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SMTP.Addr != ":25" || cfg.API.Addr != ":8080" {
		t.Fatalf("listener defaults: %+v %+v", cfg.SMTP, cfg.API)
	}
	if cfg.SMTP.MaxMessageBytes != 25<<20 {
		t.Fatalf("max message bytes = %d, want 25MiB", cfg.SMTP.MaxMessageBytes)
	}
	if cfg.SMTP.MaxPublicMessageBytes != 10<<20 {
		t.Fatalf("public max message bytes = %d, want 10MiB", cfg.SMTP.MaxPublicMessageBytes)
	}
	if cfg.SMTP.MaxRecipients != 10 || cfg.SMTP.MaxConnectionsPerIP != 10 {
		t.Fatalf("smtp limits: %+v", cfg.SMTP)
	}
	if cfg.Identity.ReservePeriod != 90*24*time.Hour {
		t.Fatalf("reserve period = %v, want 90 days", cfg.Identity.ReservePeriod)
	}
	if cfg.Identity.NamedRetention != 168*time.Hour {
		t.Fatalf("named retention = %v, want 168h", cfg.Identity.NamedRetention)
	}
	if cfg.API.MaxWaitTimeout != 120*time.Second {
		t.Fatalf("wait cap = %v, want 120s", cfg.API.MaxWaitTimeout)
	}
}

func TestLoadOverrides(t *testing.T) {
	setRequired(t)
	t.Setenv("PHENK_SMTP_ADDR", ":2525")
	t.Setenv("PHENK_SMTP_IDLE_TIMEOUT", "45s")
	t.Setenv("PHENK_BLOB_DIR", "/var/lib/phenk/blobs")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SMTP.Addr != ":2525" || cfg.SMTP.IdleTimeout != 45*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg.SMTP)
	}
	if cfg.Blob.Dir != "/var/lib/phenk/blobs" {
		t.Fatalf("blob dir = %q", cfg.Blob.Dir)
	}
}

func TestRequiredValuesAreReported(t *testing.T) {
	t.Setenv("PHENK_DATABASE_URL", "")
	t.Setenv("PHENK_MASTER_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with no database url and no master key")
	}
	// Both problems are reported at once, so an operator fixes their
	// environment in one pass rather than one variable per restart.
	msg := err.Error()
	for _, want := range []string{"PHENK_DATABASE_URL", "PHENK_MASTER_KEY"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestMasterKeyMustDecode(t *testing.T) {
	t.Setenv("PHENK_DATABASE_URL", "postgres://localhost/phenk")
	t.Setenv("PHENK_MASTER_KEY", "obviously-not-base64-of-32-bytes!!")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an unusable master key")
	}
}

func TestValidateCatchesInconsistentLimits(t *testing.T) {
	setRequired(t)
	t.Setenv("PHENK_SMTP_MAX_MESSAGE_BYTES", "1000")
	t.Setenv("PHENK_SMTP_MAX_PUBLIC_MESSAGE_BYTES", "2000")
	if _, err := Load(); err == nil {
		t.Fatal("accepted a public cap larger than the global cap")
	}
}

func TestValidateCatchesTTLInversion(t *testing.T) {
	setRequired(t)
	t.Setenv("PHENK_IDENTITY_DEFAULT_TTL", "48h")
	t.Setenv("PHENK_IDENTITY_MAX_TTL", "24h")
	if _, err := Load(); err == nil {
		t.Fatal("accepted a default ttl beyond the maximum")
	}
}

func TestValidateRequiresBothTLSFilesOrNeither(t *testing.T) {
	setRequired(t)
	t.Setenv("PHENK_SMTP_TLS_CERT_FILE", "/etc/phenk/cert.pem")
	if _, err := Load(); err == nil {
		t.Fatal("accepted a TLS certificate with no key")
	}
}

func TestValidateRejectsUnknownBlobBackend(t *testing.T) {
	setRequired(t)
	t.Setenv("PHENK_BLOB_BACKEND", "s3")
	if _, err := Load(); err == nil {
		t.Fatal("accepted a blob backend that does not exist yet")
	}
}

func TestMasterKeyBytes(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	key, err := cfg.MasterKeyBytes()
	if err != nil {
		t.Fatalf("MasterKeyBytes: %v", err)
	}
	if len(key) != crypto.KeySize {
		t.Fatalf("key is %d bytes, want %d", len(key), crypto.KeySize)
	}
}

func TestWorkerDefaults(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Worker.MaxWorkers != 4 {
		t.Errorf("max workers = %d, want 4", cfg.Worker.MaxWorkers)
	}
	if cfg.Worker.MaxAttachmentBytes != 25<<20 {
		t.Errorf("max attachment bytes = %d, want 25MiB", cfg.Worker.MaxAttachmentBytes)
	}
	if cfg.Worker.ShutdownTimeout() != 30*time.Second {
		t.Errorf("shutdown grace = %v, want 30s", cfg.Worker.ShutdownTimeout())
	}
}

func TestValidateRejectsZeroWorkers(t *testing.T) {
	setRequired(t)
	t.Setenv("PHENK_WORKER_MAX_WORKERS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("accepted a worker pool that would run nothing")
	}
}
