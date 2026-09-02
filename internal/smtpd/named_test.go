package smtpd

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

func TestNeverSeenNameOnAPublicDomainIsProvisionedLazily(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := sendMail(t, h.addr, "sender@example.com",
		[]string{"invoices@pub.test"}, message("your receipt")); err != nil {
		t.Fatalf("send: %v", err)
	}

	if n := h.countRows("identities"); n != 1 {
		t.Fatalf("%d identities, want exactly 1", n)
	}
	identity, err := pg.IdentityByAddress(ctx, h.db, "invoices", h.publicDomain.ID)
	if err != nil {
		t.Fatalf("IdentityByAddress: %v", err)
	}
	if identity.Kind != core.KindNamed || identity.State != core.IdentityActive {
		t.Fatalf("identity = %+v", identity)
	}
	// A shared inbox has no owner and no expiry, and its quotas are the
	// smaller public-pool ones.
	if identity.OwnerSession != "" || identity.ExpiresAt != nil {
		t.Fatalf("a named inbox must have no owner and no expiry: %+v", identity)
	}
	if identity.QuotaMessages != core.DefaultNamedQuotaMessages || identity.QuotaBytes != core.DefaultNamedQuotaBytes {
		t.Fatalf("quotas = %d/%d, want the named defaults", identity.QuotaMessages, identity.QuotaBytes)
	}
	if identity.RetentionHours == nil {
		t.Fatal("a named inbox needs a rolling retention window")
	}
	if len(identity.WrappedDataKey) == 0 {
		t.Fatal("a lazily provisioned identity still needs a data key")
	}

	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != 1 || deliveries[0].Seq != 1 {
		t.Fatalf("%d deliveries, want 1 at seq 1", len(deliveries))
	}

	// The identity and the delivery that caused it to exist landed together,
	// and both events were written.
	events, err := pg.EventsForIdentity(ctx, h.db, identity.ID, 0, 10)
	if err != nil {
		t.Fatalf("EventsForIdentity: %v", err)
	}
	types := map[string]int{}
	for _, e := range events {
		types[e.Type]++
	}
	if types[core.EventIdentityCreated] != 1 || types[core.EventMessageReceived] != 1 {
		t.Fatalf("events = %v, want one created and one received", types)
	}
}

func TestSecondMessageToANamedInboxDoesNotCreateASecondIdentity(t *testing.T) {
	h := newHarness(t)

	for i := 0; i < 3; i++ {
		if err := sendMail(t, h.addr, "sender@example.com",
			[]string{"invoices@pub.test"}, message(fmt.Sprintf("receipt %d", i))); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	if n := h.countRows("identities"); n != 1 {
		t.Fatalf("%d identities, want 1", n)
	}
	identity, err := pg.IdentityByAddress(context.Background(), h.db, "invoices", h.publicDomain.ID)
	if err != nil {
		t.Fatalf("IdentityByAddress: %v", err)
	}
	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != 3 {
		t.Fatalf("%d deliveries, want 3", len(deliveries))
	}
	for i, d := range deliveries {
		if want := int64(i + 1); d.Seq != want {
			t.Fatalf("delivery %d has seq %d, want %d", i, d.Seq, want)
		}
	}
}

func TestSimultaneousSendsToANewNameCreateOneIdentityAndTwoDeliveries(t *testing.T) {
	// The race the plan calls out explicitly.
	h := newHarness(t)

	const senders = 8
	var wg sync.WaitGroup
	errs := make(chan error, senders)
	start := make(chan struct{})
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := sendMail(t, h.addr, "sender@example.com",
				[]string{"invoices@pub.test"}, message(fmt.Sprintf("simultaneous %d", i))); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent send: %v", err)
	}

	if n := h.countRows("identities"); n != 1 {
		t.Fatalf("%d identities created by simultaneous senders, want 1", n)
	}
	identity, err := pg.IdentityByAddress(context.Background(), h.db, "invoices", h.publicDomain.ID)
	if err != nil {
		t.Fatalf("IdentityByAddress: %v", err)
	}
	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != senders {
		t.Fatalf("%d deliveries, want %d", len(deliveries), senders)
	}
	for i, d := range deliveries {
		if want := int64(i + 1); d.Seq != want {
			t.Fatalf("delivery %d has seq %d, want %d", i, d.Seq, want)
		}
	}

	// Exactly one identity.created event, however many senders raced.
	var created int
	events, err := pg.EventsForIdentity(context.Background(), h.db, identity.ID, 0, 100)
	if err != nil {
		t.Fatalf("EventsForIdentity: %v", err)
	}
	for _, e := range events {
		if e.Type == core.EventIdentityCreated {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d identity.created events, want 1", created)
	}
}

func TestBlockedLocalPartOnAPublicDomainIsRejected(t *testing.T) {
	h := newHarness(t)

	for _, name := range []string{"admin", "postmaster", "abuse", "security", "no-reply"} {
		err := sendMail(t, h.addr, "sender@example.com",
			[]string{name + "@pub.test"}, message("hello"))
		if code := smtpCode(t, err); code != 550 {
			t.Fatalf("%s@: got %d (%v), want 550", name, code, err)
		}
	}
	if n := h.countRows("identities"); n != 0 {
		t.Fatalf("%d identities created for blocked names", n)
	}
	if n := h.countRows("deliveries"); n != 0 {
		t.Fatalf("%d deliveries written for blocked names", n)
	}
}

func TestInvalidNamesOnAPublicDomainAreRejected(t *testing.T) {
	h := newHarness(t)

	// Too short, and a name shaped exactly like a generated address. Both are
	// refused by the same validator the HTTP path uses.
	for _, name := range []string{"ab", "k7f2m9x3qz"} {
		err := sendMail(t, h.addr, "sender@example.com",
			[]string{name + "@pub.test"}, message("hello"))
		if code := smtpCode(t, err); code != 550 {
			t.Fatalf("%s@: got %d (%v), want 550", name, code, err)
		}
	}
	if n := h.countRows("identities"); n != 0 {
		t.Fatalf("%d identities created for invalid names", n)
	}
}

func TestNamedAddressesAreNormalizedBeforeResolution(t *testing.T) {
	// INVOICES+ebay@ and invoices@ are the same inbox.
	h := newHarness(t)

	for _, rcpt := range []string{"invoices@pub.test", "INVOICES@pub.test", "invoices+ebay@PUB.test"} {
		if err := sendMail(t, h.addr, "sender@example.com", []string{rcpt}, message("hello")); err != nil {
			t.Fatalf("send to %s: %v", rcpt, err)
		}
	}

	if n := h.countRows("identities"); n != 1 {
		t.Fatalf("%d identities, want 1", n)
	}
	identity, err := pg.IdentityByAddress(context.Background(), h.db, "invoices", h.publicDomain.ID)
	if err != nil {
		t.Fatalf("IdentityByAddress: %v", err)
	}
	if got := len(h.deliveries(identity.ID)); got != 3 {
		t.Fatalf("%d deliveries, want 3", got)
	}
}

func TestProvisioningIsRateLimitedPerSource(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ProvisionsPerIPHour = 2 })

	for i := 0; i < 2; i++ {
		rcpt := fmt.Sprintf("inbox%d@pub.test", i)
		if err := sendMail(t, h.addr, "sender@example.com", []string{rcpt}, message("hello")); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	err := sendMail(t, h.addr, "sender@example.com", []string{"inbox9@pub.test"}, message("hello"))
	if code := smtpCode(t, err); code != 451 {
		t.Fatalf("got %d (%v), want 451", code, err)
	}
	if n := h.countRows("identities"); n != 2 {
		t.Fatalf("%d identities, want 2: the throttled one must not have been created", n)
	}

	// An inbox that already exists is not provisioning, so it keeps working
	// even while new names are throttled.
	if err := sendMail(t, h.addr, "sender@example.com", []string{"inbox0@pub.test"}, message("again")); err != nil {
		t.Fatalf("send to an existing inbox while throttled: %v", err)
	}
}

func TestGlobalProvisioningCapIsEnforced(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.GlobalProvisionsHour = 1 })

	if err := sendMail(t, h.addr, "sender@example.com", []string{"first@pub.test"}, message("hello")); err != nil {
		t.Fatalf("send: %v", err)
	}
	err := sendMail(t, h.addr, "sender@example.com", []string{"second@pub.test"}, message("hello"))
	if code := smtpCode(t, err); code != 451 {
		t.Fatalf("got %d (%v), want 451", code, err)
	}
	if n := h.countRows("identities"); n != 1 {
		t.Fatalf("%d identities, want 1", n)
	}
}

func TestPublicMessagesTakeTheLowerSizeCap(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.MaxMessageBytes = 4096
		c.MaxPublicMessageBytes = 1024
	})
	_, randomAddr := h.allocate("session-1")

	big := message("big") + padding(2048)
	if len(big) <= 1024 || len(big) >= 4096 {
		t.Fatalf("test message is %d bytes, which does not sit between the two caps", len(big))
	}

	// The random pool accepts it.
	if err := sendMail(t, h.addr, "sender@example.com", []string{randomAddr}, big); err != nil {
		t.Fatalf("send to the random pool: %v", err)
	}
	// The public pool does not.
	err := sendMail(t, h.addr, "sender@example.com", []string{"invoices@pub.test"}, big)
	if code := smtpCode(t, err); code != 552 {
		t.Fatalf("got %d (%v), want 552", code, err)
	}
	// A message refused at DATA must not leave the inbox it would have been
	// provisioned into behind: RCPT TO only marks the session, and creation
	// happens in the commit that never ran.
	if _, err := pg.IdentityByAddress(context.Background(), h.db, "invoices", h.publicDomain.ID); err == nil {
		t.Fatal("an oversized message provisioned an inbox anyway")
	}
}
