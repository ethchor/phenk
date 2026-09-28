package api

import (
	"net/http"
	"testing"

	"github.com/ethchor/phenk/internal/api/apigen"
)

// withoutDisposable runs the server in its default, public-only configuration.
func withoutDisposable(c *Config) { c.Disposable = false }

func TestZeroConfigLeavesPrivateAddressesOff(t *testing.T) {
	// The product default lives in the zero value, not in the harness: a
	// caller that builds a Config and forgets the field must get the
	// public-only server.
	var cfg Config
	cfg.withDefaults()
	if cfg.Disposable {
		t.Fatal("private addresses must be off unless explicitly enabled")
	}
}

func TestPrivateAddressesAreRefusedWhenTurnedOff(t *testing.T) {
	h := newHarness(t, withoutDisposable)
	c := h.client()

	var refused apigen.Error
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusForbidden, &refused)
	if refused.Error.Code != codeFeatureDisabled {
		t.Errorf("error code = %q, want %q", refused.Error.Code, codeFeatureDisabled)
	}

	// Asking for kind=random explicitly is refused the same way.
	c.decode(c.do(http.MethodPost, "/v1/identities", map[string]any{"kind": "random"}),
		http.StatusForbidden, &refused)

	// Nothing was allocated by either refusal.
	var count int
	if err := h.db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM identities`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d identities exist after two refused requests", count)
	}
}

func TestPublicInboxesStillWorkWhenPrivateOnesAreOff(t *testing.T) {
	h := newHarness(t, withoutDisposable)
	c := h.client()

	// Both doors to a public inbox stay open: the dedicated route, and the
	// generic one with kind=named.
	var opened apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "signup-test"}),
		http.StatusOK, &opened)
	if !opened.Public {
		t.Fatal("expected a public inbox")
	}

	var viaIdentities apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", map[string]any{"kind": "named", "local_part": "signup-test"}),
		http.StatusOK, &viaIdentities)
	if viaIdentities.Address != opened.Address {
		t.Fatalf("kind=named resolved to %q, want %q", viaIdentities.Address, opened.Address)
	}
}

func TestMetaReportsFeaturesAndOnlyOfferedDomains(t *testing.T) {
	t.Run("public only", func(t *testing.T) {
		h := newHarness(t, withoutDisposable)
		c := h.anonymous()

		var meta apigen.Meta
		c.decode(c.do(http.MethodGet, "/v1/meta", nil), http.StatusOK, &meta)
		if meta.Features.Disposable {
			t.Error("meta reports private addresses as on")
		}
		// The random-pool domain hands out nothing while the feature is off,
		// so it is not advertised — here or on /v1/domains.
		if len(meta.Domains) != 1 || meta.Domains[0].Pool != apigen.DomainPoolPublic {
			t.Errorf("domains = %+v, want only the public pool", meta.Domains)
		}
		if meta.PublicRetentionHours != 168 {
			t.Errorf("public retention = %dh, want the 7-day default", meta.PublicRetentionHours)
		}
		if meta.MaxWaitSeconds != 5 {
			t.Errorf("max wait = %ds, want the harness's 5s", meta.MaxWaitSeconds)
		}

		var domains []apigen.Domain
		c.decode(c.do(http.MethodGet, "/v1/domains", nil), http.StatusOK, &domains)
		if len(domains) != 1 || domains[0].Pool != apigen.DomainPoolPublic {
			t.Errorf("/v1/domains = %+v, want only the public pool", domains)
		}
	})

	t.Run("private addresses on", func(t *testing.T) {
		h := newHarness(t)
		c := h.anonymous()

		var meta apigen.Meta
		c.decode(c.do(http.MethodGet, "/v1/meta", nil), http.StatusOK, &meta)
		if !meta.Features.Disposable {
			t.Error("meta reports private addresses as off")
		}
		if len(meta.Domains) != 2 {
			t.Errorf("domains = %+v, want both pools", meta.Domains)
		}
	})
}

func TestExistingPrivateAddressesOutliveTheSwitch(t *testing.T) {
	// Switching the feature off stops new private addresses. It must not
	// strand one somebody is already using: that address was promised a
	// lifetime when it was handed out.
	h := newHarness(t)
	owner := h.client()
	var identity apigen.Identity
	owner.decode(owner.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	// What an operator restart with the flag off amounts to: same database,
	// same sessions, feature switched off. Handlers read the config per
	// request, so flipping it between requests is the same thing.
	h.server.cfg.Disposable = false

	var read apigen.Identity
	owner.decode(owner.do(http.MethodGet, "/v1/identities/"+identity.Id.String(), nil),
		http.StatusOK, &read)
	if read.Address != identity.Address {
		t.Fatalf("read back %q, want %q", read.Address, identity.Address)
	}

	var list apigen.MessageList
	owner.decode(owner.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/messages", nil),
		http.StatusOK, &list)

	// And a new one is refused.
	var refused apigen.Error
	owner.decode(owner.do(http.MethodPost, "/v1/identities", nil), http.StatusForbidden, &refused)
}
