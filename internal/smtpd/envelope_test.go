package smtpd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/pg"
)

// TestPurgingOneRecipientLeavesTheOtherReadable is the reason raw blobs use an
// envelope key rather than being encrypted under an identity key directly.
//
// One set of bytes is shared by everyone who received the message, and each
// delivery carries its own wrapping of the key that opens it. Purging an
// identity destroys its wrapping and nothing else, so its copy becomes
// unreadable while the other recipient's stays intact.
func TestPurgingOneRecipientLeavesTheOtherReadable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	purged, purgedAddr := h.allocate("session-1")
	kept, keptAddr := h.allocate("session-2")

	if err := sendMail(t, h.addr, "sender@example.com",
		[]string{purgedAddr, keptAddr}, message("shared secret 314159")); err != nil {
		t.Fatalf("send: %v", err)
	}

	purgedDelivery := h.deliveries(purged.ID)[0]
	keptDelivery := h.deliveries(kept.ID)[0]
	if purgedDelivery.BlobID != keptDelivery.BlobID {
		t.Fatal("the two recipients did not share a blob")
	}

	blobRow, err := pg.BlobByID(ctx, h.db, keptDelivery.BlobID)
	if err != nil {
		t.Fatalf("BlobByID: %v", err)
	}
	if blobRow.Refcount != 2 {
		t.Fatalf("refcount = %d, want 2", blobRow.Refcount)
	}
	stored, err := os.ReadFile(filepath.Join(h.blobDir, blobRow.Path))
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}

	// Both can read it while both hold their keys.
	if raw := h.decryptBlob(&purgedDelivery, stored); !strings.Contains(raw, "314159") {
		t.Fatal("the first recipient cannot read the message")
	}
	if raw := h.decryptBlob(&keptDelivery, stored); !strings.Contains(raw, "314159") {
		t.Fatal("the second recipient cannot read the message")
	}

	// Purge the first.
	if _, err := pg.SetIdentityState(ctx, h.db, purged.ID, core.IdentityExpired, core.IdentityActive); err != nil {
		t.Fatalf("SetIdentityState: %v", err)
	}
	if _, err := pg.DestroyDataKey(ctx, h.db, purged.ID, time.Now().Add(90*24*time.Hour)); err != nil {
		t.Fatalf("DestroyDataKey: %v", err)
	}

	// Its wrapping can no longer be opened, however many bytes survive on disk.
	purgedIdentity, err := pg.IdentityByID(ctx, h.db, purged.ID)
	if err != nil {
		t.Fatalf("IdentityByID: %v", err)
	}
	if len(purgedIdentity.WrappedDataKey) != 0 {
		t.Fatal("the purged identity kept its data key")
	}
	if _, err := h.keyring.Unwrap(purgedIdentity.ID, purgedIdentity.WrappedDataKey); err == nil {
		t.Fatal("a purged identity's data key still unwraps, so its mail is still readable")
	}

	// The other recipient is untouched.
	if raw := h.decryptBlob(&keptDelivery, stored); !strings.Contains(raw, "314159") {
		t.Fatal("purging one recipient destroyed another recipient's copy")
	}
}

func TestEachDeliveryCarriesItsOwnWrappingOfTheSameKey(t *testing.T) {
	h := newHarness(t)
	first, firstAddr := h.allocate("session-1")
	second, secondAddr := h.allocate("session-2")

	if err := sendMail(t, h.addr, "sender@example.com",
		[]string{firstAddr, secondAddr}, message("shared")); err != nil {
		t.Fatalf("send: %v", err)
	}

	a := h.deliveries(first.ID)[0]
	b := h.deliveries(second.ID)[0]
	if len(a.WrappedContentKey) == 0 || len(b.WrappedContentKey) == 0 {
		t.Fatal("a delivery is missing its content key")
	}
	// Same key, different wrappings: one identity's stored key is useless to
	// the other, which is what makes access separately revocable.
	if string(a.WrappedContentKey) == string(b.WrappedContentKey) {
		t.Fatal("both deliveries stored an identical wrapping")
	}
}
