package events

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/testsupport/pgtest"
)

var testDB *pg.DB

func TestMain(m *testing.M) {
	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "events")
	cancelSetup()
	if err == nil {
		var db *pg.DB
		openCtx, cancelOpen := context.WithTimeout(context.Background(), 15*time.Second)
		db, err = pg.Open(openCtx, dsn, 8)
		cancelOpen()
		if err == nil {
			defer db.Close()
			migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 60*time.Second)
			err = db.Migrate(migrateCtx)
			cancelMigrate()
			if err == nil {
				testDB = db
				os.Exit(m.Run())
			}
		}
	}
	if pgtest.Required() {
		panic("PHENK_TEST_REQUIRED is set but the test database is unusable: " + err.Error())
	}
	os.Stderr.WriteString("events: skipping database tests: " + err.Error() + "\n")
	os.Exit(m.Run())
}

func runningHub(t *testing.T) *Hub {
	t.Helper()
	if testDB == nil {
		t.Skip("no test database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	hub := NewHub(testDB)
	go func() { _ = hub.Run(ctx) }()
	select {
	case <-hub.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("the hub never started listening")
	}
	return hub
}

func TestSubscribeReceivesEvents(t *testing.T) {
	hub := runningHub(t)
	ctx := context.Background()
	identityID := core.NewUUID()

	notifications, unsubscribe := hub.Subscribe(identityID)
	defer unsubscribe()

	if _, err := pg.AppendEvent(ctx, testDB, &identityID, core.EventMessageReceived,
		map[string]any{"seq": 1}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	select {
	case got := <-notifications:
		if got.Type != core.EventMessageReceived {
			t.Fatalf("event type = %q", got.Type)
		}
		if got.IdentityID == nil || *got.IdentityID != identityID {
			t.Fatalf("event identity = %v", got.IdentityID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the event never arrived")
	}
}

func TestSubscribersOnlySeeTheirOwnIdentity(t *testing.T) {
	hub := runningHub(t)
	ctx := context.Background()
	mine, theirs := core.NewUUID(), core.NewUUID()

	notifications, unsubscribe := hub.Subscribe(mine)
	defer unsubscribe()

	if _, err := pg.AppendEvent(ctx, testDB, &theirs, core.EventMessageReceived, nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := pg.AppendEvent(ctx, testDB, &mine, core.EventIdentityExpiring, nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	select {
	case got := <-notifications:
		// The first thing this subscriber sees must be its own event: another
		// identity's arrived first and must not have been delivered here.
		if got.Type != core.EventIdentityExpiring {
			t.Fatalf("received another identity's event: %q", got.Type)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the event never arrived")
	}
}

func TestManySubscribersAllReceiveOneEvent(t *testing.T) {
	hub := runningHub(t)
	ctx := context.Background()
	identityID := core.NewUUID()

	const subscribers = 50
	var wg sync.WaitGroup
	received := make(chan struct{}, subscribers)
	unsubscribes := make([]func(), 0, subscribers)

	for i := 0; i < subscribers; i++ {
		notifications, unsubscribe := hub.Subscribe(identityID)
		unsubscribes = append(unsubscribes, unsubscribe)
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-notifications:
				received <- struct{}{}
			case <-time.After(15 * time.Second):
			}
		}()
	}
	defer func() {
		for _, unsubscribe := range unsubscribes {
			unsubscribe()
		}
	}()

	if hub.Subscribers(identityID) != subscribers {
		t.Fatalf("%d subscribers registered, want %d", hub.Subscribers(identityID), subscribers)
	}
	if _, err := pg.AppendEvent(ctx, testDB, &identityID, core.EventMessageReceived, nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	wg.Wait()
	close(received)
	count := 0
	for range received {
		count++
	}
	if count != subscribers {
		t.Fatalf("%d of %d subscribers received the event", count, subscribers)
	}
}

func TestASlowSubscriberDoesNotBlockTheOthers(t *testing.T) {
	// The listener must never wait on a subscriber. A browser tab that stops
	// reading has to lose notifications rather than stall every other stream
	// in the process; it catches up from its cursor when it comes back.
	hub := runningHub(t)
	ctx := context.Background()
	identityID := core.NewUUID()

	stalled, unsubscribeStalled := hub.Subscribe(identityID)
	defer unsubscribeStalled()
	_ = stalled // deliberately never read from

	attentive, unsubscribeAttentive := hub.Subscribe(identityID)
	defer unsubscribeAttentive()

	// Overflow the stalled subscriber's buffer several times over.
	for i := 0; i < subscriberBuffer*3; i++ {
		if _, err := pg.AppendEvent(ctx, testDB, &identityID, core.EventMessageReceived,
			map[string]any{"seq": i}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	// The attentive subscriber keeps receiving throughout.
	deadline := time.After(15 * time.Second)
	for i := 0; i < subscriberBuffer; i++ {
		select {
		case <-attentive:
		case <-deadline:
			t.Fatalf("the attentive subscriber stalled after %d events", i)
		}
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	hub := runningHub(t)
	identityID := core.NewUUID()

	notifications, unsubscribe := hub.Subscribe(identityID)
	if hub.Subscribers(identityID) != 1 {
		t.Fatalf("%d subscribers, want 1", hub.Subscribers(identityID))
	}

	unsubscribe()
	if hub.Subscribers(identityID) != 0 {
		t.Fatalf("%d subscribers after unsubscribe, want 0", hub.Subscribers(identityID))
	}
	if _, open := <-notifications; open {
		t.Fatal("the channel was not closed on unsubscribe")
	}
	// Unsubscribing twice must not panic on a closed channel.
	unsubscribe()
}

func TestEventsWithNoIdentityAreIgnored(t *testing.T) {
	hub := runningHub(t)
	ctx := context.Background()
	identityID := core.NewUUID()

	notifications, unsubscribe := hub.Subscribe(identityID)
	defer unsubscribe()

	if _, err := pg.AppendEvent(ctx, testDB, nil, "system.tick", nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := pg.AppendEvent(ctx, testDB, &identityID, core.EventMessageReceived, nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	select {
	case got := <-notifications:
		if got.Type != core.EventMessageReceived {
			t.Fatalf("received %q, want the identity event", got.Type)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the event never arrived")
	}
}

func TestUnsubscribingDuringDispatchIsSafe(t *testing.T) {
	// A browser tab closing while an event is being fanned out must not be
	// able to panic the listener goroutine, which would take every other
	// subscriber in the process down with it.
	hub := runningHub(t)
	ctx := context.Background()
	identityID := core.NewUUID()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Churn subscribers as fast as possible.
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				notifications, unsubscribe := hub.Subscribe(identityID)
				select {
				case <-notifications:
				default:
				}
				unsubscribe()
			}
		}()
	}

	// Fan events out into the churn.
	for i := 0; i < 60; i++ {
		if _, err := pg.AppendEvent(ctx, testDB, &identityID, core.EventMessageReceived, nil); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	// The hub is still delivering afterwards.
	notifications, unsubscribe := hub.Subscribe(identityID)
	defer unsubscribe()
	if _, err := pg.AppendEvent(ctx, testDB, &identityID, core.EventIdentityExpiring, nil); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	select {
	case <-notifications:
	case <-time.After(10 * time.Second):
		t.Fatal("the hub stopped delivering after subscriber churn")
	}
}
