package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
)

// The flow llms.txt promises an agent, end to end: wait on a name nobody has
// used, have mail arrive, read the code out of the wait response.
func TestWaitingOnAFreshNameReturnsTheCode(t *testing.T) {
	h := newHarness(t, withoutDisposable)

	type outcome struct {
		result apigen.WaitResult
		status int
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		// A plain client: t.Fatal may not be called from this goroutine.
		response, err := http.Get(h.http.URL + "/v1/named/agent-7f3c9a21/wait?timeout=5")
		if err != nil {
			done <- outcome{err: err}
			return
		}
		defer response.Body.Close()
		var result apigen.WaitResult
		err = json.NewDecoder(response.Body).Decode(&result)
		done <- outcome{result: result, status: response.StatusCode, err: err}
	}()

	// The wait opens the inbox; reopening it here is free and returns the
	// same identity whichever request got there first.
	c := h.client()
	var inbox apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "agent-7f3c9a21"}),
		http.StatusOK, &inbox)

	deliveryID := h.deliver(coreUUID(inbox.Id),
		testMessage("Your Acme code", "Hi,\n\nYour verification code is 482913.\n\nIt expires in 10 minutes."))
	h.parseDelivered(deliveryID)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("wait failed: %v", got.err)
		}
		if got.status != http.StatusOK || got.result.TimedOut || len(got.result.Messages) != 1 {
			t.Fatalf("wait returned %d %+v", got.status, got.result)
		}
		// The promise in llms.txt is that the code is in the wait response
		// itself — not in a follow-up read. A wait that answered the instant
		// the message was received, before it was parsed, would hand back
		// no subject and no code, and messages[0].extracted would be absent.
		message := got.result.Messages[0]
		if message.State != apigen.MessageSummaryStateParsed {
			t.Fatalf("wait returned a message in state %q, want parsed", message.State)
		}
		if message.Extracted == nil || len(message.Extracted.Codes) == 0 ||
			message.Extracted.Codes[0].Value != "482913" {
			t.Fatalf("extracted = %+v, want 482913 in the wait response", message.Extracted)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the wait never returned")
	}
}

func TestWaitOnAFreshNameOpensTheInboxRatherThan404ing(t *testing.T) {
	h := newHarness(t, withoutDisposable)
	c := h.anonymous()

	var result apigen.WaitResult
	c.decode(c.do(http.MethodGet, "/v1/named/never-used-before/wait?timeout=1", nil), http.StatusOK, &result)
	if !result.TimedOut || len(result.Messages) != 0 {
		t.Fatalf("result = %+v, want an empty timed-out wait", result)
	}

	// The inbox exists now, so mail to it is accepted rather than refused.
	var list apigen.MessageList
	c.decode(c.do(http.MethodGet, "/v1/named/never-used-before/messages", nil), http.StatusOK, &list)
}

func TestWaitNeverOpensAnInboxWhereOneCannotExist(t *testing.T) {
	h := newHarness(t, withoutDisposable)
	c := h.anonymous()

	for _, address := range []string{
		"someone@rand.test",       // a random-pool domain
		"someone@nowhere.example", // not a domain this server has
		"postmaster",              // reserved
		"x",                       // malformed
	} {
		response := c.do(http.MethodGet, "/v1/named/"+address+"/wait?timeout=1", nil)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", address, response.StatusCode)
		}
	}

	var count int
	if err := h.db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM identities`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d identities were created by waits that should have been refused", count)
	}
}

func TestReopeningAPublicInboxIsFree(t *testing.T) {
	// The limit is on creating inboxes. Charging for a reload would lock out
	// anyone who keeps a tab open, and any agent that waits twice.
	h := newHarness(t, withoutDisposable, func(c *Config) { c.NamedPerIPHour = 1 })
	c := h.client()

	for range 5 {
		response := c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "kept-open"})
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("reopening returned %d", response.StatusCode)
		}
		response = c.do(http.MethodGet, "/v1/named/kept-open/wait?timeout=1", nil)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("waiting on an existing inbox returned %d", response.StatusCode)
		}
	}

	// A second new name is still over the limit of one.
	response := c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "a-second-name"})
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a new name returned %d, want 429", response.StatusCode)
	}
	response = c.do(http.MethodGet, "/v1/named/a-third-name/wait?timeout=1", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("waiting on a new name returned %d, want 429", response.StatusCode)
	}
}

func TestLatestReturnsTheNewestMessageInFull(t *testing.T) {
	h := newHarness(t, withoutDisposable)
	c := h.anonymous()

	var inbox apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "latest-test"}),
		http.StatusOK, &inbox)

	// Empty inbox: nothing to read.
	response := c.do(http.MethodGet, "/v1/named/latest-test/latest", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("empty inbox returned %d, want 404", response.StatusCode)
	}

	first := h.deliver(coreUUID(inbox.Id), testMessage("first", "nothing to see"))
	h.parseDelivered(first)
	second := h.deliver(coreUUID(inbox.Id), testMessage("Sign in to Acme", "Your one-time passcode is A7K2QX"))
	h.parseDelivered(second)

	var latest apigen.Message
	c.decode(c.do(http.MethodGet, "/v1/named/latest-test/latest", nil), http.StatusOK, &latest)
	if latest.Subject != "Sign in to Acme" {
		t.Fatalf("latest = %q, want the second message", latest.Subject)
	}
	if latest.Text == nil || !strings.Contains(*latest.Text, "A7K2QX") {
		t.Fatalf("latest carried no text: %+v", latest.Text)
	}
	if latest.Extracted == nil || len(latest.Extracted.Codes) == 0 || latest.Extracted.Codes[0].Value != "A7K2QX" {
		t.Fatalf("latest.extracted = %+v, want A7K2QX", latest.Extracted)
	}
}

func TestMessagesCarryDetectedLinks(t *testing.T) {
	h := newHarness(t, withoutDisposable)
	c := h.anonymous()

	var inbox apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/named", map[string]any{"local_part": "links-test"}),
		http.StatusOK, &inbox)
	raw := "From: Acme <hello@acme.test>\r\n" +
		"Subject: Confirm your email\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" +
		`<p><a href="https://acme.test/unsubscribe">Unsubscribe</a></p>` +
		`<p><a href="https://acme.test/confirm?token=abc">Confirm email</a></p>` + "\r\n"
	delivery := h.deliver(coreUUID(inbox.Id), raw)
	h.parseDelivered(delivery)

	var message apigen.Message
	c.decode(c.do(http.MethodGet, "/v1/messages/"+delivery.String(), nil), http.StatusOK, &message)
	if message.Extracted == nil || len(message.Extracted.Links) < 2 {
		t.Fatalf("extracted = %+v, want both links", message.Extracted)
	}
	if first := message.Extracted.Links[0]; first.Kind != apigen.Verify || !strings.Contains(first.Url, "/confirm") {
		t.Fatalf("first link = %+v, want the confirm link ahead of the unsubscribe one", first)
	}
}

func TestLlmsTxtDescribesThisServer(t *testing.T) {
	h := newHarness(t, withoutDisposable, func(c *Config) { c.PublicURL = "https://phenk.example" })

	response, err := http.Get(h.http.URL + "/llms.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	if ct := response.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type %q, want text/plain", ct)
	}
	text := string(body)
	for _, want := range []string{
		"NAME@pub.test",                   // this server's public domain
		"https://phenk.example/v1/named/", // this server's base URL
		"/wait?timeout=5",                 // this server's wait ceiling
		"deleted after 168 hours",         // this server's retention
		"extracted.codes[0].value",        // the one line an agent needs
	} {
		if !strings.Contains(text, want) {
			t.Errorf("llms.txt is missing %q", want)
		}
	}
	if strings.Contains(text, "{{") {
		t.Error("llms.txt contains an unrendered template action")
	}
}
