// Package events fans Postgres notifications out to in-process subscribers.
//
// One connection holds LISTEN for the whole process. Everything else — every
// open long-poll, every SSE stream — subscribes here rather than opening a
// connection of its own, because a thousand idle browser tabs must not be a
// thousand idle database connections.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// subscriberBuffer is how many notifications a subscriber may fall behind by
// before it starts losing them.
//
// Losing them is safe and deliberate. A notification carries only a sequence
// number, and every subscriber re-reads from its cursor when it wakes, so a
// dropped notification costs a slightly later delivery and never a lost
// message. Blocking the listener instead would stall every other subscriber in
// the process, which is the one outcome that must not happen.
const subscriberBuffer = 32

// Hub distributes notifications. It is safe for concurrent use.
type Hub struct {
	db *pg.DB

	mu     sync.RWMutex
	nextID uint64
	subs   map[core.UUID]map[uint64]chan pg.Notification

	// ready is closed once LISTEN is established, so tests and callers can
	// know when a notification would actually be seen.
	readyOnce sync.Once
	ready     chan struct{}
}

// NewHub returns a hub. Call Run to start it.
func NewHub(db *pg.DB) *Hub {
	return &Hub{
		db:    db,
		subs:  make(map[core.UUID]map[uint64]chan pg.Notification),
		ready: make(chan struct{}),
	}
}

// Ready returns a channel closed once the hub is listening.
func (h *Hub) Ready() <-chan struct{} { return h.ready }

// Subscribe registers interest in one identity's events and returns the channel
// to read them from, plus the function that unregisters it. The caller must
// call unsubscribe, and must keep reading: a subscriber that stops reading
// silently loses notifications rather than holding anyone else up.
func (h *Hub) Subscribe(identityID core.UUID) (<-chan pg.Notification, func()) {
	ch := make(chan pg.Notification, subscriberBuffer)

	h.mu.Lock()
	h.nextID++
	id := h.nextID
	if h.subs[identityID] == nil {
		h.subs[identityID] = make(map[uint64]chan pg.Notification)
	}
	h.subs[identityID][id] = ch
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			// The channel is closed while the write lock is held, and dispatch
			// sends while holding the read lock. Closing outside the lock
			// would let a subscriber that hangs up mid-dispatch turn into a
			// send on a closed channel, which panics the listener goroutine
			// and takes every other subscriber down with it.
			h.mu.Lock()
			defer h.mu.Unlock()
			if group, ok := h.subs[identityID]; ok {
				delete(group, id)
				if len(group) == 0 {
					delete(h.subs, identityID)
				}
			}
			close(ch)
		})
	}
}

// Subscribers reports how many subscribers an identity has, for tests and
// metrics.
func (h *Hub) Subscribers(identityID core.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[identityID])
}

// Run holds the LISTEN connection until the context is cancelled, reconnecting
// on failure. It returns only when the context ends.
func (h *Hub) Run(ctx context.Context) error {
	backoff := 100 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for ctx.Err() == nil {
		if err := h.listen(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("event listener dropped, reconnecting", "error", err, "in", backoff)
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = 100 * time.Millisecond
	}
	return ctx.Err()
}

// listen holds one connection for as long as it lives.
func (h *Hub) listen(ctx context.Context) error {
	conn, err := h.db.Pool().Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+pg.NotifyChannel); err != nil {
		return err
	}
	slog.Debug("listening for events", "channel", pg.NotifyChannel)
	h.readyOnce.Do(func() { close(h.ready) })

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var payload pg.Notification
		if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
			slog.Warn("unreadable event notification", "payload", notification.Payload, "error", err)
			continue
		}
		h.dispatch(payload)
	}
}

// dispatch delivers a notification to every interested subscriber without ever
// waiting on one.
//
// The read lock is held across the sends. That is safe because no send here can
// block — a full subscriber is skipped rather than waited on — and it is
// necessary because it is what stops a concurrent unsubscribe from closing a
// channel this loop is about to write to.
func (h *Hub) dispatch(n pg.Notification) {
	if n.IdentityID == nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ch := range h.subs[*n.IdentityID] {
		select {
		case ch <- n:
		default:
			// The subscriber is behind. It will catch up from its cursor when
			// it next reads, so dropping is correct; blocking here would stall
			// every other subscriber in the process.
		}
	}
}
