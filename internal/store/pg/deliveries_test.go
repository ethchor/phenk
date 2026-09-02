package pg

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"sync"
	"testing"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/blob"
)

func testSHA(s string) blob.SHA256 { return blob.SHA256(sha256.Sum256([]byte(s))) }

// commit performs the delivery commit exactly as the SMTP path will: blob row,
// sequence reservation, delivery row and event in one transaction.
func commit(t *testing.T, identityID core.UUID, content string) core.Delivery {
	t.Helper()
	ctx := context.Background()
	sha := testSHA(content)
	size := int64(len(content))

	var d core.Delivery
	err := testDB.InTx(ctx, func(q Querier) error {
		blobID, _, err := AcquireBlob(ctx, q, sha, size, sha.String())
		if err != nil {
			return err
		}
		seq, err := ReserveDeliverySlot(ctx, q, identityID, size)
		if err != nil {
			return err
		}
		d = core.Delivery{
			IdentityID:   identityID,
			Seq:          seq,
			BlobID:       blobID,
			EnvelopeFrom: "sender@example.com",
			ClientIP:     netip.MustParseAddr("198.51.100.7"),
			HELO:         "mail.example.com",
			TLS:          true,
			SizeBytes:    size,
			State:        core.DeliveryReceived,
		}
		if err := InsertDelivery(ctx, q, &d); err != nil {
			return err
		}
		_, err = AppendEvent(ctx, q, &identityID, core.EventMessageReceived,
			map[string]any{"delivery_id": d.ID, "seq": seq})
		return err
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return d
}

func TestDeliveryCommitRoundTrip(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	d := commit(t, id.ID, "hello world")
	if d.Seq != 1 {
		t.Fatalf("first delivery seq = %d, want 1", d.Seq)
	}

	got, err := DeliveryByID(ctx, testDB, d.ID)
	if err != nil {
		t.Fatalf("DeliveryByID: %v", err)
	}
	if got.EnvelopeFrom != "sender@example.com" || got.HELO != "mail.example.com" || !got.TLS {
		t.Fatalf("round trip lost envelope data: %+v", got)
	}
	if got.ClientIP.String() != "198.51.100.7" {
		t.Fatalf("client ip = %s", got.ClientIP)
	}
	if got.State != core.DeliveryReceived || got.ParsedAt != nil {
		t.Fatalf("a fresh delivery should be unparsed: %+v", got)
	}

	// Usage counters were charged in the same transaction.
	after, _ := IdentityByID(ctx, testDB, id.ID)
	if after.UsedMessages != 1 || after.UsedBytes != int64(len("hello world")) {
		t.Fatalf("usage = %d messages / %d bytes", after.UsedMessages, after.UsedBytes)
	}
}

func TestOneMessageToTwoIdentitiesIsOneBlob(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)

	a := randomIdentity(rnd.ID, "aaaaaaaaaa")
	b := randomIdentity(rnd.ID, "bbbbbbbbbb")
	for _, id := range []*core.Identity{a, b} {
		if err := CreateIdentity(ctx, testDB, id); err != nil {
			t.Fatalf("CreateIdentity: %v", err)
		}
	}

	const content = "one message, two recipients"
	da := commit(t, a.ID, content)
	db := commit(t, b.ID, content)

	if da.BlobID != db.BlobID {
		t.Fatal("identical content produced two blob rows")
	}
	blobRow, err := BlobByID(ctx, testDB, da.BlobID)
	if err != nil {
		t.Fatalf("BlobByID: %v", err)
	}
	if blobRow.Refcount != 2 {
		t.Fatalf("refcount = %d, want 2", blobRow.Refcount)
	}

	var blobs int
	if err := testDB.QueryRow(ctx, `SELECT count(*) FROM blobs`).Scan(&blobs); err != nil {
		t.Fatalf("count: %v", err)
	}
	if blobs != 1 {
		t.Fatalf("%d blob rows, want 1", blobs)
	}
}

func TestConcurrentDeliveriesProduceAGaplessSequence(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	id.QuotaMessages = 1000
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	const senders = 25
	var wg sync.WaitGroup
	seqs := make([]int64, senders)
	start := make(chan struct{})
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			d := commit(t, id.ID, fmt.Sprintf("message %d", i))
			seqs[i] = d.Seq
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[int64]bool{}
	for _, s := range seqs {
		if seen[s] {
			t.Fatalf("sequence number %d was handed out twice", s)
		}
		seen[s] = true
	}
	for want := int64(1); want <= senders; want++ {
		if !seen[want] {
			t.Fatalf("sequence %d is missing: the cursor has a gap", want)
		}
	}
}

func TestRolledBackDeliveryConsumesNothing(t *testing.T) {
	// A session that dies mid-DATA must leave no delivery row, no usage, and no
	// hole in the sequence.
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	commit(t, id.ID, "first")

	failure := fmt.Errorf("connection dropped mid-DATA")
	err := testDB.InTx(ctx, func(q Querier) error {
		if _, _, err := AcquireBlob(ctx, q, testSHA("doomed"), 6, "doomed"); err != nil {
			return err
		}
		if _, err := ReserveDeliverySlot(ctx, q, id.ID, 6); err != nil {
			return err
		}
		return failure
	})
	if err == nil {
		t.Fatal("the doomed transaction committed")
	}

	after, _ := IdentityByID(ctx, testDB, id.ID)
	if after.DeliverySeq != 1 || after.UsedMessages != 1 {
		t.Fatalf("the aborted delivery left state behind: seq=%d used=%d", after.DeliverySeq, after.UsedMessages)
	}
	var blobs int
	_ = testDB.QueryRow(ctx, `SELECT count(*) FROM blobs`).Scan(&blobs)
	if blobs != 1 {
		t.Fatalf("%d blob rows, want 1: the aborted blob was not rolled back", blobs)
	}

	// The next real delivery takes seq 2, with no gap.
	if d := commit(t, id.ID, "second"); d.Seq != 2 {
		t.Fatalf("next seq = %d, want 2", d.Seq)
	}
}

func TestDeliveriesSincePagesByCursor(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}
	for i := 0; i < 5; i++ {
		commit(t, id.ID, fmt.Sprintf("message %d", i))
	}

	all, err := DeliveriesSince(ctx, testDB, id.ID, 0, 100)
	if err != nil {
		t.Fatalf("DeliveriesSince: %v", err)
	}
	if len(all) != 5 || all[0].Seq != 1 || all[4].Seq != 5 {
		t.Fatalf("got %d deliveries starting at %d", len(all), all[0].Seq)
	}

	rest, err := DeliveriesSince(ctx, testDB, id.ID, 3, 100)
	if err != nil || len(rest) != 2 || rest[0].Seq != 4 {
		t.Fatalf("cursor paging: %d rows, first seq %d, err %v", len(rest), rest[0].Seq, err)
	}

	page, err := DeliveriesSince(ctx, testDB, id.ID, 0, 2)
	if err != nil || len(page) != 2 {
		t.Fatalf("limit: %d rows, err %v", len(page), err)
	}

	caughtUp, err := DeliveriesSince(ctx, testDB, id.ID, 5, 100)
	if err != nil || len(caughtUp) != 0 {
		t.Fatalf("a caught-up cursor returned %d rows", len(caughtUp))
	}
}

func TestMarkDeliveryParsedAndFailed(t *testing.T) {
	reset(t)
	ctx := context.Background()
	rnd, _ := fixtures(t)
	id := randomIdentity(rnd.ID, "k7f2m9x3qz")
	if err := CreateIdentity(ctx, testDB, id); err != nil {
		t.Fatalf("CreateIdentity: %v", err)
	}

	ok := commit(t, id.ID, "parses fine")
	if err := MarkDeliveryParsed(ctx, testDB, ok.ID, core.AuthPass, core.AuthPass, core.AuthFail); err != nil {
		t.Fatalf("MarkDeliveryParsed: %v", err)
	}
	got, _ := DeliveryByID(ctx, testDB, ok.ID)
	if got.State != core.DeliveryParsed || got.ParsedAt == nil {
		t.Fatalf("after parse: %+v", got)
	}
	if got.SPF != core.AuthPass || got.DKIM != core.AuthPass || got.DMARC != core.AuthFail {
		t.Fatalf("auth results not stored: %+v", got)
	}

	bad := commit(t, id.ID, "does not parse")
	if err := MarkDeliveryFailed(ctx, testDB, bad.ID); err != nil {
		t.Fatalf("MarkDeliveryFailed: %v", err)
	}
	got, _ = DeliveryByID(ctx, testDB, bad.ID)
	if got.State != core.DeliveryFailed {
		t.Fatalf("after failure: %+v", got)
	}
	// The blob is kept regardless, so the raw message stays readable.
	if _, err := BlobByID(ctx, testDB, bad.BlobID); err != nil {
		t.Fatalf("the blob of a failed parse was lost: %v", err)
	}
}
