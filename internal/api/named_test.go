package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
)

func TestOpenNamedInboxIsStableAndPublic(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var opened apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "Invoices+ebay"}),
		http.StatusOK, &opened)

	if opened.LocalPart != "invoices" {
		t.Errorf("local part = %q, want the normalized name", opened.LocalPart)
	}
	if !strings.HasSuffix(opened.Address, "@pub.test") {
		t.Errorf("address = %q, want a public-pool domain", opened.Address)
	}
	if opened.Kind != apigen.IdentityKindNamed {
		t.Errorf("kind = %q", opened.Kind)
	}
	// The flag every interface uses to warn that anyone who guesses the name
	// can read this inbox.
	if !opened.Public {
		t.Error("a named inbox must report itself as public")
	}
	if opened.ExpiresAt != nil {
		t.Error("a named inbox must not expire")
	}
	if opened.RetentionHours == nil {
		t.Error("a named inbox needs a rolling retention window")
	}
	if opened.Quota.Messages != core.DefaultNamedQuotaMessages {
		t.Errorf("quota = %+v, want the smaller named allowance", opened.Quota)
	}

	// The same name always resolves to the same address, for anyone.
	stranger := h.anonymous()
	var again apigen.Identity
	stranger.decode(stranger.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "invoices"}),
		http.StatusOK, &again)
	if again.Address != opened.Address {
		t.Fatalf("a second caller got %q, want %q", again.Address, opened.Address)
	}
}

func TestNamedInboxNeedsNoOwnership(t *testing.T) {
	// A public inbox is public. Anyone who knows the name can read it, and the
	// UI is required to say so.
	h := newHarness(t)
	creator := h.client()

	var opened apigen.Identity
	creator.decode(creator.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "invoices"}),
		http.StatusOK, &opened)
	deliveryID := h.deliver(coreUUID(opened.Id), testMessage("a public message", "body"))
	h.parseDelivered(deliveryID)

	stranger := h.anonymous()
	var list apigen.MessageList
	stranger.decode(stranger.do(http.MethodGet, "/v1/named/invoices/messages", nil), http.StatusOK, &list)
	if len(list.Messages) != 1 || list.Messages[0].Subject != "a public message" {
		t.Fatalf("a stranger could not read a public inbox: %+v", list.Messages)
	}

	// A full address works too, and so does a +tag.
	var byAddress apigen.MessageList
	stranger.decode(stranger.do(http.MethodGet, "/v1/named/invoices%2Bebay@pub.test/messages", nil),
		http.StatusOK, &byAddress)
	if len(byAddress.Messages) != 1 {
		t.Fatalf("resolving by full address gave %d messages", len(byAddress.Messages))
	}
}

func TestNamedEndpointsRefuseARandomIdentity(t *testing.T) {
	// The authorization test the plan calls for: every named endpoint refuses
	// a random identity. Enforced in the handler, not only by route, so a
	// guessed name can never reach a private inbox through the public door.
	h := newHarness(t)
	owner := h.client()

	var random apigen.Identity
	owner.decode(owner.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &random)
	h.deliver(coreUUID(random.Id), testMessage("private", "secret"))

	stranger := h.anonymous()
	for _, path := range []string{
		"/v1/named/" + random.LocalPart + "/messages",
		"/v1/named/" + random.Address + "/messages",
		"/v1/named/" + random.LocalPart + "/stream",
	} {
		response := stranger.do(http.MethodGet, path, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s returned %d, want 404: a random inbox is reachable through the public door",
				path, response.StatusCode)
		}
	}
}

func TestRandomEndpointsRefuseANamedIdentity(t *testing.T) {
	// And the other direction: a named identity must not be reachable where
	// ownership is implied, because it has no owner to imply.
	h := newHarness(t)
	c := h.client()

	var named apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "invoices"}),
		http.StatusOK, &named)

	for _, path := range []string{
		"/v1/identities/" + named.Id.String(),
		"/v1/identities/" + named.Id.String() + "/messages",
		"/v1/identities/" + named.Id.String() + "/wait?timeout=1",
	} {
		response := c.do(http.MethodGet, path, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s returned %d, want 404", path, response.StatusCode)
		}
	}
	response := c.do(http.MethodDelete, "/v1/identities/"+named.Id.String(), nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("a named inbox could be destroyed through the owner route: %d", response.StatusCode)
	}
}

func TestNamedValidationMatchesTheSMTPPath(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	rejected := []string{"ab", "admin", "postmaster", "no-reply", "k7f2m9x3qz", ".leading", "has space", ""}
	for _, name := range rejected {
		response := c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": name})
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("local_part %q returned %d, want 400", name, response.StatusCode)
		}
	}

	var count int
	if err := h.db.QueryRow(t.Context(), `SELECT count(*) FROM identities`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d identities created by rejected names", count)
	}
}

func TestNamedRejectionDoesNotRecitTheDenylist(t *testing.T) {
	// Naming which list a name is on would turn the error into a way to
	// enumerate it.
	h := newHarness(t)
	c := h.client()

	response := c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "postmaster"})
	var body apigen.Error
	c.decode(response, http.StatusBadRequest, &body)
	if strings.Contains(strings.ToLower(body.Error.Message), "blocked") ||
		strings.Contains(strings.ToLower(body.Error.Message), "denylist") {
		t.Fatalf("the rejection describes the list: %q", body.Error.Message)
	}
}

func TestOpeningANamedInboxIsRateLimited(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.NamedPerIPHour = 2 })
	c := h.client()

	for _, name := range []string{"first", "second"} {
		response := c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": name})
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s returned %d", name, response.StatusCode)
		}
	}
	response := c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "third"})
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third request returned %d, want 429", response.StatusCode)
	}
}

func TestCreateIdentityWithKindNamedGoesThroughTheSamePath(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var created apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities",
		map[string]any{"kind": "named", "local_part": "invoices"}), http.StatusCreated, &created)

	if created.Kind != apigen.IdentityKindNamed || !created.Public {
		t.Fatalf("identity = %+v", created)
	}
	if !strings.HasSuffix(created.Address, "@pub.test") {
		t.Fatalf("address = %q, want a public-pool domain", created.Address)
	}

	// And it is validated the same way.
	response := c.do(http.MethodPost, "/v1/identities", map[string]any{"kind": "named", "local_part": "admin"})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("a blocked name returned %d, want 400", response.StatusCode)
	}

	response = c.do(http.MethodPost, "/v1/identities", map[string]any{"kind": "named"})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("a named request with no local_part returned %d, want 400", response.StatusCode)
	}
}
