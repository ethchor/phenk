package api

import (
	"net/http"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
)

// defaultWaitTimeout is used when a caller names none.
const defaultWaitTimeout = 30 * time.Second

// WaitForMessages implements apigen.ServerInterface.
func (s *Server) WaitForMessages(w http.ResponseWriter, r *http.Request, id apigen.IdentityId, params apigen.WaitForMessagesParams) {
	identity, _, ok := s.ownedIdentity(w, r, id)
	if !ok {
		return
	}
	s.wait(w, r, identity, since(params.Since), waitTimeout(params.Timeout, s.cfg.MaxWaitTimeout))
}

// wait holds a request open until a message arrives after the cursor.
//
// The subscription is taken out *before* the database is queried, and the
// ordering matters. Invariant 6 exists because the gap between creating an
// address and first waiting on it is where real mail lands, and querying first
// certainly catches that. But querying first and subscribing second leaves a
// second gap — a message committed between the two would go unnoticed until the
// timeout expired, which is the very failure the invariant is guarding against.
// Subscribing first and then querying catches both: anything already there is
// returned immediately, and anything arriving from this instant on wakes the
// subscription.
func (s *Server) wait(w http.ResponseWriter, r *http.Request, identity *core.Identity, sinceSeq int64, timeout time.Duration) {
	notifications, unsubscribe := s.hub.Subscribe(identity.ID)
	defer unsubscribe()

	summaries, cursor, err := s.messagePage(r, identity, sinceSeq, defaultPageSize)
	if err != nil {
		internalError(w, r, "waiting for messages", err)
		return
	}
	if len(summaries) > 0 {
		writeJSON(w, http.StatusOK, apigen.WaitResult{Messages: summaries, Cursor: cursor, TimedOut: false})
		return
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case <-r.Context().Done():
			// The caller hung up. Nothing to write.
			return

		case <-deadline.C:
			writeJSON(w, http.StatusOK, apigen.WaitResult{
				Messages: []apigen.MessageSummary{}, Cursor: sinceSeq, TimedOut: true,
			})
			return

		case notification, open := <-notifications:
			if !open {
				writeJSON(w, http.StatusOK, apigen.WaitResult{
					Messages: []apigen.MessageSummary{}, Cursor: sinceSeq, TimedOut: true,
				})
				return
			}
			// Only a new message ends the wait. Parse completions and
			// lifecycle events travel the same channel and would otherwise
			// end it early with nothing to show.
			if notification.Type != core.EventMessageReceived {
				continue
			}
			summaries, cursor, err = s.messagePage(r, identity, sinceSeq, defaultPageSize)
			if err != nil {
				internalError(w, r, "waiting for messages", err)
				return
			}
			if len(summaries) == 0 {
				// A notification for something already past our cursor.
				continue
			}
			writeJSON(w, http.StatusOK, apigen.WaitResult{Messages: summaries, Cursor: cursor, TimedOut: false})
			return
		}
	}
}

// waitTimeout clamps a requested timeout to the server maximum. Holding a
// request open costs a connection at both ends, so the ceiling is the server's
// to choose.
func waitTimeout(requested *int, max time.Duration) time.Duration {
	if requested == nil || *requested <= 0 {
		return min(defaultWaitTimeout, max)
	}
	timeout := time.Duration(*requested) * time.Second
	if timeout > max {
		return max
	}
	return timeout
}
