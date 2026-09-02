package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/sanitize"
)

func TestCreateAndReadAnIdentity(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var created apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", map[string]any{"ttl_seconds": 3600}),
		http.StatusCreated, &created)

	if !strings.HasSuffix(created.Address, "@rand.test") {
		t.Fatalf("address = %q", created.Address)
	}
	if created.Kind != apigen.IdentityKindRandom || created.State != apigen.Active {
		t.Fatalf("identity = %+v", created)
	}
	if created.Public {
		t.Fatal("a random address must not be reported as public")
	}
	if created.ExpiresAt == nil {
		t.Fatal("a random address must expire")
	}
	if created.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0 on a new address", created.Cursor)
	}
	if created.Quota.Messages != core.DefaultRandomQuotaMessages {
		t.Fatalf("quota = %+v", created.Quota)
	}

	var fetched apigen.Identity
	c.decode(c.do(http.MethodGet, "/v1/identities/"+created.Id.String(), nil), http.StatusOK, &fetched)
	if fetched.Address != created.Address {
		t.Fatalf("re-read gave %q, want %q", fetched.Address, created.Address)
	}
}

func TestAnotherSessionCannotSeeYourInbox(t *testing.T) {
	// The single most important authorization rule in v0: a random address
	// belongs to the session that made it and to nobody else.
	h := newHarness(t)
	owner := h.client()

	var created apigen.Identity
	owner.decode(owner.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &created)
	h.deliver(coreUUID(created.Id), testMessage("private", "secret"))

	stranger := h.anonymous()
	for _, path := range []string{
		"/v1/identities/" + created.Id.String(),
		"/v1/identities/" + created.Id.String() + "/messages",
		"/v1/identities/" + created.Id.String() + "/wait?timeout=1",
	} {
		response := stranger.do(http.MethodGet, path, nil)
		response.Body.Close()
		// 404 rather than 403: telling a stranger the address exists would let
		// them enumerate who uses the service.
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s returned %d to a stranger, want 404", path, response.StatusCode)
		}
	}

	response := stranger.do(http.MethodDelete, "/v1/identities/"+created.Id.String(), nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("a stranger could destroy the address: %d", response.StatusCode)
	}
}

func TestListMessages(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	first := h.deliver(coreUUID(identity.Id), testMessage("first", "one"))
	h.parseDelivered(first)
	h.deliver(coreUUID(identity.Id), testMessage("second", "two"))

	var list apigen.MessageList
	c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/messages", nil),
		http.StatusOK, &list)

	if len(list.Messages) != 2 {
		t.Fatalf("%d messages, want 2", len(list.Messages))
	}
	if list.Messages[0].Subject != "first" {
		t.Errorf("first message subject = %q", list.Messages[0].Subject)
	}
	// A message that has not been parsed yet still appears, with what is known
	// of it: an inbox that looked empty for a second after mail arrived would
	// be worse than one showing a message without a subject.
	if list.Messages[1].State != apigen.MessageSummaryStateReceived {
		t.Errorf("second message state = %q, want received", list.Messages[1].State)
	}
	if list.Messages[1].From.Address != "sender@example.com" {
		t.Errorf("an unparsed message should still show its envelope sender: %+v", list.Messages[1].From)
	}
	if list.Cursor != 2 {
		t.Errorf("cursor = %d, want 2", list.Cursor)
	}

	// Paging from the cursor returns nothing and leaves the cursor alone.
	var caughtUp apigen.MessageList
	c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/messages?since=2", nil),
		http.StatusOK, &caughtUp)
	if len(caughtUp.Messages) != 0 || caughtUp.Cursor != 2 {
		t.Fatalf("caught up page = %+v", caughtUp)
	}
}

func TestReadAMessage(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)
	deliveryID := h.deliver(coreUUID(identity.Id), htmlMessage("verify"))
	h.parseDelivered(deliveryID)

	var message apigen.Message
	c.decode(c.do(http.MethodGet, "/v1/messages/"+deliveryID.String(), nil), http.StatusOK, &message)

	if message.Subject != "verify" {
		t.Errorf("subject = %q", message.Subject)
	}
	if message.Html == nil {
		t.Fatal("no html body returned")
	}
	html := *message.Html
	// The API returns already-sanitized markup. It is still only safe inside a
	// sandboxed iframe, and the schema says so, but nothing executable should
	// have survived this far.
	if strings.Contains(html, "<script") || strings.Contains(html, "alert(1)") {
		t.Errorf("script survived into the API response: %s", html)
	}
	if !strings.Contains(html, sanitize.ImageProxyPrefix) {
		t.Errorf("the tracking pixel was not proxied: %s", html)
	}
	if strings.Contains(html, "tracker.test/pixel.gif") && !strings.Contains(html, sanitize.ImageProxyPrefix) {
		t.Error("the tracking pixel still loads directly")
	}
	if !strings.Contains(html, "481920") {
		t.Error("the verification code was lost")
	}
}

func TestReadRawMessage(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)
	raw := testMessage("raw", "the original bytes")
	deliveryID := h.deliver(coreUUID(identity.Id), raw)

	response := c.do(http.MethodGet, "/v1/messages/"+deliveryID.String()+"/raw", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "message/rfc822" {
		t.Errorf("content type = %q", got)
	}
	// Bytes from a stranger are downloaded, never displayed.
	if got := response.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("content disposition = %q, want an attachment", got)
	}
	if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("x-content-type-options = %q", got)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	// The raw message round-trips through envelope encryption unchanged: it is
	// the record everything else is derived from.
	if string(body) != raw {
		t.Fatalf("raw message came back changed:\n%q\nwant\n%q", body, raw)
	}
}

func TestDestroyAnIdentity(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	response := c.do(http.MethodDelete, "/v1/identities/"+identity.Id.String(), nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d, want 204", response.StatusCode)
	}

	// Repeating it is safe: the caller's intent already holds.
	response = c.do(http.MethodDelete, "/v1/identities/"+identity.Id.String(), nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("repeat destroy returned %d, want 204", response.StatusCode)
	}

	var fetched apigen.Identity
	c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String(), nil), http.StatusOK, &fetched)
	if fetched.State != apigen.Expired {
		t.Fatalf("state = %q, want expired", fetched.State)
	}
}

func TestWaitReturnsAMessageThatArrivedBeforeTheCall(t *testing.T) {
	// Invariant 6, and the acceptance criterion for this phase. The gap between
	// creating an address and first waiting on it is exactly where real mail
	// lands, so a wait that only ever watched the future would miss it.
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	deliveryID := h.deliver(coreUUID(identity.Id), testMessage("already here", "body"))
	h.parseDelivered(deliveryID)

	started := time.Now()
	var result apigen.WaitResult
	c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/wait?timeout=5", nil),
		http.StatusOK, &result)

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("wait took %v for a message that had already arrived", elapsed)
	}
	if result.TimedOut {
		t.Fatal("wait timed out despite a message being there")
	}
	if len(result.Messages) != 1 || result.Messages[0].Subject != "already here" {
		t.Fatalf("wait returned %+v", result.Messages)
	}
	if result.Cursor != 1 {
		t.Fatalf("cursor = %d, want 1", result.Cursor)
	}
}

func TestWaitWakesOnANewMessage(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	done := make(chan apigen.WaitResult, 1)
	go func() {
		var result apigen.WaitResult
		c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/wait?timeout=10", nil),
			http.StatusOK, &result)
		done <- result
	}()

	// Give the waiter time to subscribe, then deliver.
	time.Sleep(250 * time.Millisecond)
	h.deliver(coreUUID(identity.Id), testMessage("just arrived", "body"))

	select {
	case result := <-done:
		if result.TimedOut || len(result.Messages) != 1 {
			t.Fatalf("wait returned %+v", result)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("wait never woke for a delivered message")
	}
}

func TestWaitTimesOutCleanly(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	var result apigen.WaitResult
	c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/wait?timeout=1", nil),
		http.StatusOK, &result)

	if !result.TimedOut || len(result.Messages) != 0 {
		t.Fatalf("wait returned %+v", result)
	}
	// The cursor must not move on an empty wait, or the next call would skip
	// a message committed a moment later.
	if result.Cursor != 0 {
		t.Fatalf("cursor = %d, want it left where it was", result.Cursor)
	}
}

func TestManyConcurrentWaitersAllReceiveTheMessage(t *testing.T) {
	// The acceptance criterion: 50 waiters on one identity all see one
	// delivery. It is really a test that the notification fan-out never blocks
	// and never drops a waiter.
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	const waiters = 50
	results := make(chan apigen.WaitResult, waiters)
	var ready sync.WaitGroup
	ready.Add(waiters)

	for i := 0; i < waiters; i++ {
		go func() {
			ready.Done()
			var result apigen.WaitResult
			c.decode(c.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/wait?timeout=20", nil),
				http.StatusOK, &result)
			results <- result
		}()
	}
	ready.Wait()
	time.Sleep(500 * time.Millisecond)

	h.deliver(coreUUID(identity.Id), testMessage("broadcast", "one message, fifty readers"))

	deadline := time.After(30 * time.Second)
	for i := 0; i < waiters; i++ {
		select {
		case result := <-results:
			if result.TimedOut || len(result.Messages) != 1 {
				t.Fatalf("waiter %d got %+v", i, result)
			}
			if result.Messages[0].Seq != 1 {
				t.Fatalf("waiter %d saw seq %d", i, result.Messages[0].Seq)
			}
		case <-deadline:
			t.Fatalf("only %d of %d waiters were served", i, waiters)
		}
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	c := h.anonymous()

	var health apigen.Health
	c.decode(c.do(http.MethodGet, "/healthz", nil), http.StatusOK, &health)
	if health.Status != apigen.Ok {
		t.Fatalf("status = %q", health.Status)
	}
}

func TestUnknownIdentityIsNotFound(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	for _, path := range []string{
		"/v1/identities/" + core.NewUUID().String(),
		"/v1/identities/not-a-uuid",
		"/v1/messages/" + core.NewUUID().String(),
	} {
		response := c.do(http.MethodGet, path, nil)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s returned %d: %s", path, response.StatusCode, body)
		}
	}
}

func TestErrorsAreStructured(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	response := c.do(http.MethodGet, "/v1/identities/"+core.NewUUID().String(), nil)
	defer response.Body.Close()

	var body apigen.Error
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the error: %v", err)
	}
	if body.Error.Code == "" || body.Error.Message == "" {
		t.Fatalf("error = %+v, want a code and a message", body.Error)
	}
}
