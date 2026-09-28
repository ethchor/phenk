package api

import (
	"net/http"
	"time"

	"github.com/ethchor/phenk/internal/api/apigen"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// defaultWaitTimeout is used when a caller names none.
const defaultWaitTimeout = 30 * time.Second

// defaultParseGrace bounds how long a wait holds messages it already has while
// they are parsed. Parsing takes well under a second; the bound is for a parse
// worker that is down, which must not turn every wait into a full timeout.
const defaultParseGrace = 10 * time.Second

// settlePoll re-checks held messages on a timer as well as on events, because a
// parse that fails marks the delivery failed without announcing it.
const settlePoll = 400 * time.Millisecond

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
//
// Once there is something to return, the wait holds it briefly until it is
// parsed. A message that has only been received has no subject, no preview and
// no detected code, and the caller of a wait — usually an agent about to read
// messages[0].extracted — has no use for it in that state.
func (s *Server) wait(w http.ResponseWriter, r *http.Request, identity *core.Identity, sinceSeq int64, timeout time.Duration) {
	notifications, unsubscribe := s.hub.Subscribe(identity.ID)
	defer unsubscribe()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	summaries, cursor, err := s.messagePage(r, identity, sinceSeq, defaultPageSize)
	if err != nil {
		internalError(w, r, "waiting for messages", err)
		return
	}

	for len(summaries) == 0 {
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
			// An empty page here is a notification for something already
			// past our cursor; the loop keeps waiting.
		}
	}

	summaries, cursor, ok := s.settle(w, r, identity, sinceSeq, summaries, cursor, notifications, deadline.C)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, apigen.WaitResult{Messages: summaries, Cursor: cursor, TimedOut: false})
}

// settle holds messages until every one has been parsed or has failed to
// parse, then returns the refreshed page. It gives up — returning the messages
// as they stand — after parseGrace or at the wait's own deadline, whichever
// comes first: a wait that has mail always answers with it. It reports false
// only when the caller has gone, or when it has already written an error.
func (s *Server) settle(
	w http.ResponseWriter, r *http.Request, identity *core.Identity, sinceSeq int64,
	summaries []apigen.MessageSummary, cursor int64,
	notifications <-chan pg.Notification, deadline <-chan time.Time,
) ([]apigen.MessageSummary, int64, bool) {
	if allParsed(summaries) {
		return summaries, cursor, true
	}

	grace := time.NewTimer(s.cfg.ParseGrace)
	defer grace.Stop()
	poll := time.NewTicker(settlePoll)
	defer poll.Stop()

	for {
		select {
		case <-r.Context().Done():
			return nil, 0, false
		case <-grace.C:
			return summaries, cursor, true
		case <-deadline:
			return summaries, cursor, true
		case notification, open := <-notifications:
			if !open {
				return summaries, cursor, true
			}
			if notification.Type != core.EventMessageParsed && notification.Type != core.EventMessageReceived {
				continue
			}
		case <-poll.C:
		}

		refreshed, refreshedCursor, err := s.messagePage(r, identity, sinceSeq, defaultPageSize)
		if err != nil {
			internalError(w, r, "waiting for messages", err)
			return nil, 0, false
		}
		summaries, cursor = refreshed, refreshedCursor
		if allParsed(summaries) {
			return summaries, cursor, true
		}
	}
}

// allParsed reports whether every message has left the received state, parsed
// or failed.
func allParsed(summaries []apigen.MessageSummary) bool {
	for _, summary := range summaries {
		if summary.State == apigen.MessageSummaryStateReceived {
			return false
		}
	}
	return true
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
