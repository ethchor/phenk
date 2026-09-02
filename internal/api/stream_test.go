package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
)

// readEvents reads server-sent events off a live stream until the context ends.
func readEvents(t *testing.T, ctx context.Context, response *http.Response) <-chan string {
	t.Helper()
	events := make(chan string, 32)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				select {
				case events <- strings.TrimPrefix(line, "event: "):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return events
}

func TestStreamDeliversNewMessages(t *testing.T) {
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		h.http.URL+"/v1/identities/"+identity.Id.String()+"/stream", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for _, cookie := range c.http.Jar.Cookies(nil) {
		request.AddCookie(cookie)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}

	events := readEvents(t, ctx, response)

	// Give the stream a moment to subscribe, then deliver.
	time.Sleep(300 * time.Millisecond)
	h.deliver(coreUUID(identity.Id), testMessage("streamed", "body"))

	select {
	case event := <-events:
		if event != "message.received" {
			t.Fatalf("first event was %q", event)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no event arrived for a delivered message")
	}
}

func TestStreamReplaysFromTheCursorOnReconnect(t *testing.T) {
	// A client that was disconnected when mail arrived must still see it, or a
	// dropped connection silently loses a message.
	h := newHarness(t)
	c := h.client()

	var identity apigen.Identity
	c.decode(c.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)
	h.deliver(coreUUID(identity.Id), testMessage("missed while away", "body"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		h.http.URL+"/v1/identities/"+identity.Id.String()+"/stream?since=0", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for _, cookie := range c.http.Jar.Cookies(nil) {
		request.AddCookie(cookie)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	defer response.Body.Close()

	events := readEvents(t, ctx, response)
	select {
	case event := <-events:
		if event != "message.received" {
			t.Fatalf("replayed event was %q", event)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stream did not replay a message that arrived while disconnected")
	}
}

func TestStreamRefusesAnotherSession(t *testing.T) {
	h := newHarness(t)
	owner := h.client()

	var identity apigen.Identity
	owner.decode(owner.do(http.MethodPost, "/v1/identities", nil), http.StatusCreated, &identity)

	stranger := h.anonymous()
	response := stranger.do(http.MethodGet, "/v1/identities/"+identity.Id.String()+"/stream", nil)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", response.StatusCode)
	}
}
