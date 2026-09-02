package api

import (
	"bytes"
	"context"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/store/pg"
)

// deliver commits a message to an identity exactly as the SMTP path does,
// including the envelope encryption, so the API is exercised against the shape
// of data it will really see.
func (h *harness) deliver(identityID core.UUID, raw string) core.UUID {
	h.t.Helper()
	ctx := context.Background()

	identity, err := pg.IdentityByID(ctx, h.db, identityID)
	if err != nil {
		h.t.Fatalf("IdentityByID: %v", err)
	}
	dataKey, err := h.keyring.Unwrap(identity.ID, identity.WrappedDataKey)
	if err != nil {
		h.t.Fatalf("Unwrap: %v", err)
	}
	defer dataKey.Destroy()

	contentKey, rawContentKey, err := crypto.NewContentKey()
	if err != nil {
		h.t.Fatalf("NewContentKey: %v", err)
	}
	var sealed bytes.Buffer
	if _, err := contentKey.SealStream(&sealed, bytes.NewReader([]byte(raw))); err != nil {
		h.t.Fatalf("SealStream: %v", err)
	}
	wrapped, err := dataKey.Seal(rawContentKey)
	if err != nil {
		h.t.Fatalf("Seal: %v", err)
	}

	sha, storedSize, err := h.blobs.Put(ctx, bytes.NewReader(sealed.Bytes()))
	if err != nil {
		h.t.Fatalf("Put: %v", err)
	}

	var deliveryID core.UUID
	err = h.db.InTx(ctx, func(q pg.Querier) error {
		blobID, _, err := pg.AcquireBlob(ctx, q, sha, storedSize, h.blobs.Locate(sha))
		if err != nil {
			return err
		}
		seq, err := pg.ReserveDeliverySlot(ctx, q, identityID, int64(len(raw)))
		if err != nil {
			return err
		}
		d := &core.Delivery{
			IdentityID:        identityID,
			Seq:               seq,
			BlobID:            blobID,
			EnvelopeFrom:      "sender@example.com",
			ClientIP:          mustAddr("198.51.100.7"),
			HELO:              "mail.example.com",
			SizeBytes:         int64(len(raw)),
			State:             core.DeliveryReceived,
			WrappedContentKey: wrapped,
		}
		if err := pg.InsertDelivery(ctx, q, d); err != nil {
			return err
		}
		deliveryID = d.ID
		_, err = pg.AppendEvent(ctx, q, &identityID, core.EventMessageReceived,
			map[string]any{"delivery_id": d.ID, "seq": seq})
		return err
	})
	if err != nil {
		h.t.Fatalf("delivery commit: %v", err)
	}
	return deliveryID
}

// parseDelivered runs the real parser over a delivery, so tests that need a
// subject and a body get one the same way production does.
func (h *harness) parseDelivered(deliveryID core.UUID) {
	h.t.Helper()
	if err := h.parser().Parse(context.Background(), deliveryID); err != nil {
		h.t.Fatalf("Parse: %v", err)
	}
}
