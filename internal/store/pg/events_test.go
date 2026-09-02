package pg

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/jackc/pgx/v5"
)

func TestAppendAndReadEvents(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	first, err := AppendEvent(ctx, testDB, &id.ID, core.EventIdentityCreated, map[string]any{"address": "k7f2m9x3qz@rand.test"})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	second, err := AppendEvent(ctx, testDB, &id.ID, core.EventMessageReceived, map[string]any{"seq": 1})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if second <= first {
		t.Fatalf("event sequence went backwards: %d then %d", first, second)
	}

	// An event with no payload and no identity is still valid: lifecycle jobs
	// emit those.
	if _, err := AppendEvent(ctx, testDB, nil, "system.tick", nil); err != nil {
		t.Fatalf("AppendEvent without payload: %v", err)
	}

	events, err := EventsSince(ctx, testDB, 0, 100)
	if err != nil {
		t.Fatalf("EventsSince: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["address"] != "k7f2m9x3qz@rand.test" {
		t.Fatalf("payload = %v", payload)
	}

	mine, err := EventsForIdentity(ctx, testDB, id.ID, 0, 100)
	if err != nil || len(mine) != 2 {
		t.Fatalf("EventsForIdentity: %d events, err %v", len(mine), err)
	}

	after, err := EventsSince(ctx, testDB, second, 100)
	if err != nil || len(after) != 1 || after[0].Type != "system.tick" {
		t.Fatalf("cursor paging: %+v %v", after, err)
	}
}

func TestAppendEventNotifiesOnCommitOnly(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	listener, err := testDB.Pool().Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer listener.Release()
	if _, err := listener.Exec(ctx, "LISTEN "+NotifyChannel); err != nil {
		t.Fatalf("LISTEN: %v", err)
	}

	// A rolled-back event must announce nothing.
	rollback := errNotified
	_ = testDB.InTx(ctx, func(q Querier) error {
		if _, err := AppendEvent(ctx, q, &id.ID, core.EventMessageReceived, nil); err != nil {
			return err
		}
		return rollback
	})

	// A committed one must.
	if _, err := AppendEvent(ctx, testDB, &id.ID, core.EventIdentityExpiring, map[string]any{"seq": 7}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	notification, err := listener.Conn().WaitForNotification(waitCtx)
	if err != nil {
		t.Fatalf("WaitForNotification: %v", err)
	}

	var got Notification
	if err := json.Unmarshal([]byte(notification.Payload), &got); err != nil {
		t.Fatalf("notification payload %q: %v", notification.Payload, err)
	}
	if got.Type != core.EventIdentityExpiring {
		t.Fatalf("first notification was %q: the rolled-back event was announced", got.Type)
	}
	if got.IdentityID == nil || *got.IdentityID != id.ID {
		t.Fatalf("notification identity = %v", got.IdentityID)
	}
	if got.Seq == 0 {
		t.Fatal("notification carried no event sequence")
	}

	// Nothing else is pending: exactly one notification was sent.
	drainCtx, cancelDrain := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancelDrain()
	if extra, err := listener.Conn().WaitForNotification(drainCtx); err == nil {
		t.Fatalf("a second notification arrived: %s", extra.Payload)
	}
}

var errNotified = pgx.ErrTxClosed // any non-nil error rolls the transaction back
