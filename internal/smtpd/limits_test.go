package smtpd

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

func TestOverQuotaIsRefusedAtRcpt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	identity, address := h.allocate("session-1")

	// Two messages fit; the third does not.
	if _, err := h.db.Exec(ctx, `UPDATE identities SET quota_messages = 2 WHERE id = $1`, identity.ID); err != nil {
		t.Fatalf("setting quota: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := sendMail(t, h.addr, "sender@example.com", []string{address}, message(fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("one too many"))
	if code := smtpCode(t, err); code != 452 {
		t.Fatalf("got %d (%v), want 452", code, err)
	}
	if !strings.Contains(strings.ToLower(smtpMessage(t, err)), "full") {
		t.Fatalf("reply was %q, want a mailbox full message", smtpMessage(t, err))
	}
	if got := len(h.deliveries(identity.ID)); got != 2 {
		t.Fatalf("%d deliveries, want 2: the refused message was stored anyway", got)
	}
}

func TestByteQuotaIsEnforcedAtCommit(t *testing.T) {
	// The message count is known at RCPT TO but the size is not, so the byte
	// quota can only be enforced once the bytes have arrived.
	h := newHarness(t)
	ctx := context.Background()
	identity, address := h.allocate("session-1")

	if _, err := h.db.Exec(ctx, `UPDATE identities SET quota_bytes = 50 WHERE id = $1`, identity.ID); err != nil {
		t.Fatalf("setting quota: %v", err)
	}

	err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("far larger than fifty bytes"))
	if code := smtpCode(t, err); code != 452 {
		t.Fatalf("got %d (%v), want 452", code, err)
	}
	if got := len(h.deliveries(identity.ID)); got != 0 {
		t.Fatalf("%d deliveries, want 0", got)
	}
	after, err := pg.IdentityByID(ctx, h.db, identity.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if after.UsedMessages != 0 || after.UsedBytes != 0 || after.DeliverySeq != 0 {
		t.Fatalf("the refused message consumed state: %+v", after)
	}
}

func TestTooManyRecipientsIsRefused(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxRecipients = 3 })

	addresses := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		_, address := h.allocate(fmt.Sprintf("session-%d", i))
		addresses = append(addresses, address)
	}

	err := sendMail(t, h.addr, "sender@example.com", addresses, message("too many"))
	if code := smtpCode(t, err); code != 452 {
		t.Fatalf("got %d (%v), want 452", code, err)
	}
}

func TestRepeatedRecipientIsOneDelivery(t *testing.T) {
	h := newHarness(t)
	identity, address := h.allocate("session-1")

	if err := sendMail(t, h.addr, "sender@example.com",
		[]string{address, address, strings.ToUpper(address)}, message("duplicated")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := len(h.deliveries(identity.ID)); got != 1 {
		t.Fatalf("%d deliveries, want 1", got)
	}
}

func TestConnectionsPerSourceAreCapped(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxConnectionsPerIP = 2 })

	// Hold the cap open with two idle connections.
	held := make([]net.Conn, 0, 2)
	for i := 0; i < 2; i++ {
		conn, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		held = append(held, conn)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()

	// Let the server accept both before the third arrives.
	for _, c := range held {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 256)
		if _, err := c.Read(buf); err != nil {
			t.Fatalf("greeting: %v", err)
		}
	}

	third, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer third.Close()
	_ = third.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 256)
	n, err := third.Read(buf)
	if err != nil {
		t.Fatalf("reading the refusal: %v", err)
	}
	if !strings.HasPrefix(string(buf[:n]), "421") {
		t.Fatalf("got %q, want a 421 refusal", strings.TrimSpace(string(buf[:n])))
	}

	// Closing one connection frees a slot again.
	_ = held[0].Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		fourth, err := net.DialTimeout("tcp", h.addr, 5*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		_ = fourth.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := fourth.Read(buf)
		greeting := ""
		if err == nil {
			greeting = string(buf[:n])
		}
		_ = fourth.Close()
		if strings.HasPrefix(greeting, "220") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the slot was never released, last greeting %q", strings.TrimSpace(greeting))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOversizedMessageIsRefused(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.MaxMessageBytes = 2048
		c.MaxPublicMessageBytes = 2048
	})
	identity, address := h.allocate("session-1")

	big := message("big") + padding(4096)
	err := sendMail(t, h.addr, "sender@example.com", []string{address}, big)
	if code := smtpCode(t, err); code != 552 {
		t.Fatalf("got %d (%v), want 552", code, err)
	}
	if got := len(h.deliveries(identity.ID)); got != 0 {
		t.Fatalf("%d deliveries stored for an oversized message", got)
	}
	if n := h.countRows("blobs"); n != 0 {
		t.Fatalf("%d blobs stored for an oversized message", n)
	}
}

func TestMalformedAddressesAreRefused(t *testing.T) {
	h := newHarness(t)

	err := sendMail(t, h.addr, "sender@example.com", []string{"not-an-address"}, message("hello"))
	if code := smtpCode(t, err); code != 501 && code != 550 {
		t.Fatalf("got %d (%v), want a permanent rejection", code, err)
	}
}

func TestEmptyReversePathIsAccepted(t *testing.T) {
	// Bounces and delivery status notifications arrive with an empty reverse
	// path. Refusing them would mean losing exactly the messages that explain
	// why other mail failed.
	h := newHarness(t)
	identity, address := h.allocate("session-1")

	if err := sendMail(t, h.addr, "", []string{address}, message("bounce")); err != nil {
		t.Fatalf("send: %v", err)
	}
	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != 1 {
		t.Fatalf("%d deliveries, want 1", len(deliveries))
	}
	if deliveries[0].EnvelopeFrom != "" {
		t.Fatalf("envelope from = %q, want empty", deliveries[0].EnvelopeFrom)
	}
}

func TestBurnedDomainsStillReceiveButRetiredOnesDoNot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	identity, address := h.allocate("session-1")

	if err := pg.SetDomainState(ctx, h.db, h.randomDomain.ID, core.DomainBurned); err != nil {
		t.Fatalf("SetDomainState: %v", err)
	}
	if err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("still mine")); err != nil {
		t.Fatalf("a burned domain must still deliver to identities it hosts: %v", err)
	}
	if got := len(h.deliveries(identity.ID)); got != 1 {
		t.Fatalf("%d deliveries, want 1", got)
	}

	if err := pg.SetDomainState(ctx, h.db, h.randomDomain.ID, core.DomainRetired); err != nil {
		t.Fatalf("SetDomainState: %v", err)
	}
	err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("too late"))
	if code := smtpCode(t, err); code != 550 {
		t.Fatalf("got %d (%v), want 550 from a retired domain", code, err)
	}
}

func TestConnectionLimiter(t *testing.T) {
	limiter := newConnectionLimiter(2)
	ip := netip.MustParseAddr("198.51.100.7")
	other := netip.MustParseAddr("198.51.100.8")

	if !limiter.Acquire(ip) || !limiter.Acquire(ip) {
		t.Fatal("the first two acquisitions should succeed")
	}
	if limiter.Acquire(ip) {
		t.Fatal("the third acquisition should be refused")
	}
	// The cap is per source, not global.
	if !limiter.Acquire(other) {
		t.Fatal("a different source should have its own budget")
	}

	limiter.Release(ip)
	if !limiter.Acquire(ip) {
		t.Fatal("a released slot should be reusable")
	}

	// Releasing more than was acquired is harmless.
	for i := 0; i < 5; i++ {
		limiter.Release(other)
	}
	if !limiter.Acquire(other) {
		t.Fatal("over-releasing broke the limiter")
	}
}

func TestConnectionLimiterIsConcurrencySafe(t *testing.T) {
	limiter := newConnectionLimiter(50)
	ip := netip.MustParseAddr("198.51.100.7")

	var wg sync.WaitGroup
	var granted sync.Map
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if limiter.Acquire(ip) {
				granted.Store(i, true)
			}
		}(i)
	}
	wg.Wait()

	count := 0
	granted.Range(func(_, _ any) bool { count++; return true })
	if count != 50 {
		t.Fatalf("%d acquisitions granted, want exactly the limit of 50", count)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	limiter := newRateLimiter(3, time.Hour)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !limiter.Allow("a") {
			t.Fatalf("draw %d should have been allowed", i)
		}
	}
	if limiter.Allow("a") {
		t.Fatal("the budget should be spent")
	}
	// Keys are independent.
	if !limiter.Allow("b") {
		t.Fatal("a different key should have its own budget")
	}

	// A third of the window refills one token.
	now = now.Add(21 * time.Minute)
	if !limiter.Allow("a") {
		t.Fatal("a token should have refilled")
	}
	if limiter.Allow("a") {
		t.Fatal("only one token should have refilled")
	}
}
