package smtpd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

func TestDeliverToAKnownAddress(t *testing.T) {
	h := newHarness(t)
	identity, address := h.allocate("session-1")

	if err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("hello")); err != nil {
		t.Fatalf("send: %v", err)
	}

	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != 1 {
		t.Fatalf("%d deliveries, want 1", len(deliveries))
	}
	d := deliveries[0]
	if d.Seq != 1 || d.State != core.DeliveryReceived {
		t.Fatalf("delivery = %+v", d)
	}
	if d.EnvelopeFrom != "sender@example.com" || d.HELO != "sender.test" {
		t.Fatalf("envelope not recorded: from=%q helo=%q", d.EnvelopeFrom, d.HELO)
	}
	if !d.ClientIP.IsLoopback() {
		t.Fatalf("client ip = %s", d.ClientIP)
	}
	if d.SizeBytes <= 0 {
		t.Fatal("delivery recorded no size")
	}

	// The raw bytes are on disk, content-addressed, and encrypted.
	blobRow, err := pg.BlobByID(context.Background(), h.db, d.BlobID)
	if err != nil {
		t.Fatalf("BlobByID: %v", err)
	}
	if blobRow.Refcount != 1 {
		t.Fatalf("blob = %+v", blobRow)
	}
	// The blob row counts what was stored, which carries encryption overhead;
	// the delivery counts the message itself, which is what a quota charges.
	if blobRow.SizeBytes <= d.SizeBytes {
		t.Fatalf("stored %d bytes for a %d byte message, want the ciphertext to be larger",
			blobRow.SizeBytes, d.SizeBytes)
	}

	stored, err := os.ReadFile(filepath.Join(h.blobDir, blobRow.Path))
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if strings.Contains(string(stored), "Subject: hello") {
		t.Fatal("the raw message is stored in the clear")
	}

	// And it decrypts, through the delivery's own wrapping of the content key.
	if len(d.WrappedContentKey) == 0 {
		t.Fatal("the delivery carries no content key")
	}
	if raw := h.decryptBlob(&d, stored); !strings.Contains(raw, "Subject: hello") {
		t.Fatalf("decrypted message = %q", raw)
	}

	// The received event was written in the same transaction.
	events, err := pg.EventsForIdentity(context.Background(), h.db, identity.ID, 0, 10)
	if err != nil {
		t.Fatalf("EventsForIdentity: %v", err)
	}
	var received int
	for _, e := range events {
		if e.Type == core.EventMessageReceived {
			received++
		}
	}
	if received != 1 {
		t.Fatalf("%d message.received events, want 1", received)
	}
}

func TestUnknownAddressIsRejectedAndWritesNothing(t *testing.T) {
	h := newHarness(t)

	err := sendMail(t, h.addr, "sender@example.com",
		[]string{"nobodyhere@rand.test"}, message("hello"))
	if code := smtpCode(t, err); code != 550 {
		t.Fatalf("got %d (%v), want 550", code, err)
	}

	if n := h.countRows("deliveries"); n != 0 {
		t.Fatalf("%d deliveries written for an unknown address", n)
	}
	if n := h.countRows("blobs"); n != 0 {
		t.Fatalf("%d blobs written for an unknown address", n)
	}
	if n := h.countRows("identities"); n != 0 {
		t.Fatalf("%d identities created for an unknown address", n)
	}
}

func TestNeverSeenNameOnARandomDomainIsRejected(t *testing.T) {
	// Lazy provisioning is a public-pool feature. On a random-pool domain an
	// unknown local part is simply not a user here, whatever it looks like.
	h := newHarness(t)

	err := sendMail(t, h.addr, "sender@example.com",
		[]string{"invoices@rand.test"}, message("hello"))
	if code := smtpCode(t, err); code != 550 {
		t.Fatalf("got %d (%v), want 550", code, err)
	}
	if n := h.countRows("identities"); n != 0 {
		t.Fatalf("%d identities created on the random pool", n)
	}
	if n := h.countRows("deliveries"); n != 0 {
		t.Fatalf("%d deliveries written", n)
	}
}

func TestExpiredAndPurgedAddressesLookExactlyLikeUnknownOnes(t *testing.T) {
	// The rejection text must not distinguish an address that never existed
	// from one that expired, or the SMTP surface becomes an oracle for
	// enumerating who used the service.
	h := newHarness(t)
	ctx := context.Background()

	expired, expiredAddr := h.allocate("session-1")
	if _, err := pg.SetIdentityState(ctx, h.db, expired.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}

	purged, purgedAddr := h.allocate("session-2")
	if _, err := pg.SetIdentityState(ctx, h.db, purged.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	if _, err := pg.DestroyDataKey(ctx, h.db, purged.ID, time.Now().Add(90*24*time.Hour)); err != nil {
		t.Fatalf("DestroyDataKey: %v", err)
	}

	unknownErr := sendMail(t, h.addr, "sender@example.com", []string{"neverexisted@rand.test"}, message("x"))
	expiredErr := sendMail(t, h.addr, "sender@example.com", []string{expiredAddr}, message("x"))
	purgedErr := sendMail(t, h.addr, "sender@example.com", []string{purgedAddr}, message("x"))

	for name, err := range map[string]error{"unknown": unknownErr, "expired": expiredErr, "purged": purgedErr} {
		if code := smtpCode(t, err); code != 550 {
			t.Fatalf("%s: got %d, want 550", name, code)
		}
	}
	if smtpMessage(t, expiredErr) != smtpMessage(t, unknownErr) {
		t.Fatalf("an expired address replies %q but an unknown one replies %q",
			smtpMessage(t, expiredErr), smtpMessage(t, unknownErr))
	}
	if smtpMessage(t, purgedErr) != smtpMessage(t, unknownErr) {
		t.Fatalf("a purged address replies %q but an unknown one replies %q",
			smtpMessage(t, purgedErr), smtpMessage(t, unknownErr))
	}
	if n := h.countRows("deliveries"); n != 0 {
		t.Fatalf("%d deliveries written", n)
	}
}

func TestUnknownDomainIsRefusedAsRelay(t *testing.T) {
	h := newHarness(t)

	err := sendMail(t, h.addr, "sender@example.com",
		[]string{"anyone@somewhere-else.test"}, message("hello"))
	if code := smtpCode(t, err); code != 550 {
		t.Fatalf("got %d (%v), want 550", code, err)
	}
	if msg := smtpMessage(t, err); !strings.Contains(strings.ToLower(msg), "relay") {
		t.Fatalf("reply was %q, want a relay refusal", msg)
	}
}

func TestOneMessageToTwoIdentitiesIsOneBlobAndTwoDeliveries(t *testing.T) {
	h := newHarness(t)
	first, firstAddr := h.allocate("session-1")
	second, secondAddr := h.allocate("session-2")

	if err := sendMail(t, h.addr, "sender@example.com",
		[]string{firstAddr, secondAddr}, message("shared")); err != nil {
		t.Fatalf("send: %v", err)
	}

	firstDeliveries := h.deliveries(first.ID)
	secondDeliveries := h.deliveries(second.ID)
	if len(firstDeliveries) != 1 || len(secondDeliveries) != 1 {
		t.Fatalf("deliveries: %d and %d, want one each", len(firstDeliveries), len(secondDeliveries))
	}
	if firstDeliveries[0].BlobID != secondDeliveries[0].BlobID {
		t.Fatal("the same message produced two blobs")
	}
	if n := h.countRows("blobs"); n != 1 {
		t.Fatalf("%d blob rows, want 1", n)
	}

	blobRow, err := pg.BlobByID(context.Background(), h.db, firstDeliveries[0].BlobID)
	if err != nil {
		t.Fatalf("BlobByID: %v", err)
	}
	if blobRow.Refcount != 2 {
		t.Fatalf("refcount = %d, want 2", blobRow.Refcount)
	}
}

func TestDroppingTheConnectionMidDataCommitsNothing(t *testing.T) {
	// The fault the plan calls out: a process that dies between the start of
	// DATA and the commit must leave no delivery row and no orphaned blob.
	h := newHarness(t)
	identity, address := h.allocate("session-1")

	s := dialRaw(t, h.addr)
	s.command("EHLO sender.test", "250")
	s.command("MAIL FROM:<sender@example.com>", "250")
	s.command(fmt.Sprintf("RCPT TO:<%s>", address), "250")
	s.command("DATA", "354")
	s.send("Subject: half a message")
	s.send("")
	s.send("This message will never be term")
	// No terminating dot: the connection simply goes away.
	s.close()

	// Give the server a moment to notice and unwind.
	time.Sleep(200 * time.Millisecond)

	if got := h.deliveries(identity.ID); len(got) != 0 {
		t.Fatalf("%d deliveries committed from an abandoned session", len(got))
	}
	if n := h.countRows("blobs"); n != 0 {
		t.Fatalf("%d blob rows from an abandoned session", n)
	}

	// No stray bytes on disk either: the blob is only written at commit.
	var files int
	_ = filepath.Walk(h.blobDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Fatalf("%d files written to blob storage from an abandoned session", files)
	}

	// The identity is untouched, and the next real message still takes seq 1.
	after, err := pg.IdentityByID(context.Background(), h.db, identity.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if after.DeliverySeq != 0 || after.UsedMessages != 0 || after.UsedBytes != 0 {
		t.Fatalf("the abandoned session consumed state: %+v", after)
	}
	if err := sendMail(t, h.addr, "sender@example.com", []string{address}, message("real")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := h.deliveries(identity.ID); len(got) != 1 || got[0].Seq != 1 {
		t.Fatalf("next delivery = %+v, want seq 1", got)
	}
}

func TestConcurrentSendsProduceAGaplessSequence(t *testing.T) {
	// Every sender here shares one source address, so the per-IP connection cap
	// has to be lifted for this test. TestConnectionsPerSourceAreCapped covers
	// the cap itself.
	const senders = 20
	h := newHarness(t, func(c *Config) { c.MaxConnectionsPerIP = senders + 5 })
	identity, address := h.allocate("session-1")

	var wg sync.WaitGroup
	errs := make(chan error, senders)
	start := make(chan struct{})
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if err := sendMail(t, h.addr, "sender@example.com", []string{address},
				message(fmt.Sprintf("message %d", i))); err != nil {
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

	deliveries := h.deliveries(identity.ID)
	if len(deliveries) != senders {
		t.Fatalf("%d deliveries, want %d", len(deliveries), senders)
	}
	for i, d := range deliveries {
		if want := int64(i + 1); d.Seq != want {
			t.Fatalf("delivery %d has seq %d, want %d: the cursor has a gap", i, d.Seq, want)
		}
	}
}
