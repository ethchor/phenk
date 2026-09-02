package alloc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
)

// TestPhase1Acceptance is the acceptance criterion for the foundation phase:
// create an identity, allocate an address, encrypt and decrypt a payload under
// its data key, and destroy the key.
//
// It runs the pieces together rather than in isolation because the thing worth
// proving is that they compose: the key the allocator mints is the key the
// store returns, and destroying it really does make the content unreadable.
func TestPhase1Acceptance(t *testing.T) {
	db := requireDB(t)
	ctx := context.Background()
	seedDomains(t, db, "rand.test")

	keyring := testKeyring(t)
	allocator := New(keyring, Options{DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour})

	// 1. Allocate an address. The identity, its key and its creation event all
	//    commit together.
	result, err := allocator.AllocateRandom(ctx, db, "session-acceptance", time.Hour)
	if err != nil {
		t.Fatalf("AllocateRandom: %v", err)
	}
	identity := result.Identity
	t.Logf("allocated %s", result.Address())

	// 2. Store a raw message in the blob store, exactly as the SMTP path will:
	//    content-addressed, and encrypted under the identity's own key.
	blobs, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	dataKey, err := keyring.Unwrap(identity.ID, identity.WrappedDataKey)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}

	raw := []byte("From: sender@example.com\r\nSubject: your code is 481920\r\n\r\nhello")
	sealed, err := dataKey.Seal(raw)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, []byte("481920")) {
		t.Fatal("the stored bytes contain the plaintext")
	}

	sha, size, err := blobs.Put(ctx, bytes.NewReader(sealed))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// 3. Commit a delivery the way the SMTP path will: blob row, sequence
	//    reservation, delivery row and event in one transaction.
	var deliverySeq int64
	err = db.InTx(ctx, func(q pg.Querier) error {
		blobID, _, err := pg.AcquireBlob(ctx, q, sha, size, blobs.Locate(sha))
		if err != nil {
			return err
		}
		seq, err := pg.ReserveDeliverySlot(ctx, q, identity.ID, size)
		if err != nil {
			return err
		}
		deliverySeq = seq
		d := &core.Delivery{
			IdentityID:   identity.ID,
			Seq:          seq,
			BlobID:       blobID,
			EnvelopeFrom: "sender@example.com",
			ClientIP:     mustAddr("198.51.100.7"),
			HELO:         "mail.example.com",
			TLS:          true,
			SizeBytes:    size,
			State:        core.DeliveryReceived,
		}
		if err := pg.InsertDelivery(ctx, q, d); err != nil {
			return err
		}
		_, err = pg.AppendEvent(ctx, q, &identity.ID, core.EventMessageReceived,
			map[string]any{"seq": seq})
		return err
	})
	if err != nil {
		t.Fatalf("delivery commit: %v", err)
	}
	if deliverySeq != 1 {
		t.Fatalf("first delivery seq = %d, want 1", deliverySeq)
	}

	// 4. Read it back the way the API will: reload the wrapped key from the
	//    row, unwrap, fetch the blob, decrypt.
	reloaded, err := pg.IdentityByID(ctx, db, identity.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	readKey, err := keyring.Unwrap(reloaded.ID, reloaded.WrappedDataKey)
	if err != nil {
		t.Fatalf("Unwrap after reload: %v", err)
	}
	rc, err := blobs.Get(ctx, sha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stored, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	plaintext, err := readKey.Open(stored)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(plaintext, raw) {
		t.Fatalf("decrypted %q, want %q", plaintext, raw)
	}

	// 5. Purge. Destroying the key is what destroys the content: the bytes on
	//    disk can stay exactly where they are and still be unreadable.
	if _, err := pg.SetIdentityState(ctx, db, identity.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	purged, err := pg.DestroyDataKey(ctx, db, identity.ID, time.Now().Add(90*24*time.Hour))
	if err != nil || !purged {
		t.Fatalf("DestroyDataKey: %v %v", purged, err)
	}

	afterPurge, err := pg.IdentityByID(ctx, db, identity.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if len(afterPurge.WrappedDataKey) != 0 {
		t.Fatal("the wrapped key survived the purge")
	}
	if _, err := keyring.Unwrap(afterPurge.ID, afterPurge.WrappedDataKey); err == nil {
		t.Fatal("a destroyed key still unwraps")
	}

	// The in-memory key is destroyed too, and the ciphertext it produced is now
	// unreadable by anyone.
	readKey.Destroy()
	if _, err := readKey.Open(stored); !errors.Is(err, crypto.ErrKeyDestroyed) {
		t.Fatalf("Open after Destroy = %v, want ErrKeyDestroyed", err)
	}

	// 6. The address is a tombstone: it can never be handed out again.
	if _, err := allocatorReuse(ctx, db, keyring, afterPurge); !errors.Is(err, pg.ErrConflict) {
		t.Fatalf("reallocating a purged address: got %v, want ErrConflict", err)
	}
}

// allocatorReuse tries to create a second identity at an address that has
// already been purged.
func allocatorReuse(ctx context.Context, db *pg.DB, keyring *crypto.Keyring, purged *core.Identity) (*core.Identity, error) {
	id := core.NewUUID()
	_, wrapped, err := keyring.NewDataKey(id)
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(time.Hour)
	identity := &core.Identity{
		ID:             id,
		LocalPart:      purged.LocalPart,
		DomainID:       purged.DomainID,
		Kind:           core.KindRandom,
		State:          core.IdentityActive,
		OwnerSession:   "a-different-session",
		WrappedDataKey: wrapped,
		QuotaMessages:  core.DefaultRandomQuotaMessages,
		QuotaBytes:     core.DefaultRandomQuotaBytes,
		ExpiresAt:      &expires,
	}
	return identity, pg.CreateIdentity(ctx, db, identity)
}
