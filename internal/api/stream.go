package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
)

// heartbeatInterval keeps a stream alive through proxies that close idle
// connections, and lets a client notice a dead server rather than waiting
// forever for mail that will never arrive.
const heartbeatInterval = 25 * time.Second

// StreamIdentity implements apigen.ServerInterface.
func (s *Server) StreamIdentity(w http.ResponseWriter, r *http.Request, id apigen.IdentityId, params apigen.StreamIdentityParams) {
	identity, _, ok := s.ownedIdentity(w, r, id)
	if !ok {
		return
	}
	s.stream(w, r, identity, since(params.Since))
}

// stream serves server-sent events for one inbox.
//
// As with wait, the subscription is taken out before the catch-up query, so a
// message committed between the two is delivered by the subscription rather
// than missed until the next reconnect.
func (s *Server) stream(w http.ResponseWriter, r *http.Request, identity *core.Identity, sinceSeq int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		internalError(w, r, "streaming", fmt.Errorf("the response writer cannot flush"))
		return
	}

	notifications, unsubscribe := s.hub.Subscribe(identity.ID)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// Buffering proxies turn a live stream into a very slow batch job.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	cursor := sinceSeq

	// Catch up on anything that arrived while the client was away, so a
	// reconnect does not silently skip a message.
	summaries, next, err := s.messagePage(r, identity, cursor, defaultPageSize)
	if err == nil {
		for i := range summaries {
			writeEvent(w, flusher, core.EventMessageReceived, summaries[i])
		}
		cursor = next
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-heartbeat.C:
			// A comment line is a valid event-stream frame that clients ignore.
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()

		case notification, open := <-notifications:
			if !open {
				return
			}
			switch notification.Type {
			case core.EventMessageReceived, core.EventMessageParsed:
				summaries, next, err := s.messagePage(r, identity, cursor, defaultPageSize)
				if err != nil {
					return
				}
				for i := range summaries {
					writeEvent(w, flusher, notification.Type, summaries[i])
				}
				if len(summaries) > 0 {
					cursor = next
				}
				if notification.Type == core.EventMessageParsed && len(summaries) == 0 {
					// The message was already sent as received; tell the
					// client to re-read it now that it has a subject.
					writeEvent(w, flusher, notification.Type, map[string]any{"cursor": cursor})
				}
			default:
				// Lifecycle events carry no message, only the fact that they
				// happened.
				writeEvent(w, flusher, notification.Type, map[string]any{"cursor": cursor})
			}
		}
	}
}

// writeEvent emits one server-sent event.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, body); err != nil {
		return
	}
	flusher.Flush()
}
