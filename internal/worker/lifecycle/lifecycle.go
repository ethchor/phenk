// Package lifecycle runs the jobs that make an ephemeral address ephemeral.
//
// Every transition here is guarded by a state predicate in the WHERE clause
// rather than by a check in Go. That is what makes each job idempotent: running
// it twice matches nothing the second time, so a retry, an overlapping tick, or
// two workers racing all converge on the same state and emit each event once.
//
// The order the jobs run in is a deliberate progression, and nothing skips a
// step:
//
//	active → expiring → expired → reserved
//
// A reserved identity is a tombstone. It is never deleted, because the row is
// the only thing that stops the address being handed to somebody else.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
)

// Batch sizes bound how much one tick does, so a backlog is worked through over
// several ticks rather than in one transaction that holds locks for minutes.
const (
	defaultBatchSize    = 500
	defaultBlobBatch    = 1000
	defaultNamedBatch   = 200
	defaultRetentionMax = 500
)

// Options configures the lifecycle jobs.
type Options struct {
	// PurgeGrace is how long an expired identity is kept before its key is
	// destroyed. It exists so that a user who expires an address by accident,
	// or a client mid-request when the deadline passes, is not reading a
	// message that vanishes underneath them.
	PurgeGrace time.Duration

	// ReservePeriod is how long a purged address is recorded as reserved. The
	// row outlives it: the period is a record of when the reservation was
	// made, not a licence to reuse the address afterwards.
	ReservePeriod time.Duration

	// ExpiringNotice is how far ahead of expiry the warning is emitted.
	ExpiringNotice time.Duration

	// NamedRetention is the default rolling window for a named inbox whose own
	// retention_hours is unset.
	NamedRetention time.Duration

	BatchSize int
}

func (o *Options) withDefaults() {
	if o.PurgeGrace <= 0 {
		o.PurgeGrace = 5 * time.Minute
	}
	if o.ReservePeriod <= 0 {
		o.ReservePeriod = 90 * 24 * time.Hour
	}
	if o.ExpiringNotice <= 0 {
		o.ExpiringNotice = 5 * time.Minute
	}
	if o.NamedRetention <= 0 {
		o.NamedRetention = time.Duration(core.DefaultNamedRetentionHours) * time.Hour
	}
	if o.BatchSize <= 0 {
		o.BatchSize = defaultBatchSize
	}
}

// Runner performs the lifecycle jobs.
type Runner struct {
	db    *pg.DB
	blobs blob.Store
	opts  Options

	// now is injectable so tests can move time without sleeping.
	now func() time.Time
}

// New builds a Runner.
func New(db *pg.DB, blobs blob.Store, opts Options) *Runner {
	opts.withDefaults()
	return &Runner{db: db, blobs: blobs, opts: opts, now: time.Now}
}

// Notice emits identity.expiring for addresses about to lapse, and returns how
// many were notified.
//
// The move to the expiring state is the idempotency guard: an identity already
// in it is not matched again, so the notice is emitted exactly once no matter
// how often the job runs. Named identities have no expiry and are excluded by
// the query rather than by a special case.
func (r *Runner) Notice(ctx context.Context) (int, error) {
	threshold := r.now().Add(r.opts.ExpiringNotice)

	var notified []core.UUID
	err := r.db.InTx(ctx, func(q pg.Querier) error {
		ids, err := pg.MarkExpiringIdentities(ctx, q, threshold, r.opts.BatchSize)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := pg.AppendEvent(ctx, q, &id, core.EventIdentityExpiring,
				map[string]any{"in_seconds": int(r.opts.ExpiringNotice.Seconds())}); err != nil {
				return err
			}
		}
		notified = ids
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("lifecycle: notifying expiring identities: %w", err)
	}
	if len(notified) > 0 {
		slog.Info("notified expiring identities", "count", len(notified))
	}
	return len(notified), nil
}

// Expire moves identities past their deadline out of service. They stop
// accepting mail immediately; their contents survive until Purge runs.
func (r *Runner) Expire(ctx context.Context) (int, error) {
	var expired []core.UUID
	err := r.db.InTx(ctx, func(q pg.Querier) error {
		ids, err := pg.ExpireDueIdentities(ctx, q, r.now(), r.opts.BatchSize)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := pg.AppendEvent(ctx, q, &id, core.EventIdentityExpired, nil); err != nil {
				return err
			}
		}
		expired = ids
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("lifecycle: expiring identities: %w", err)
	}
	if len(expired) > 0 {
		slog.Info("expired identities", "count", len(expired))
	}
	return len(expired), nil
}

// Purge destroys the contents of expired identities.
//
// The key is what actually destroys the data. Rows and bytes are removed too,
// but a blob that survives a crash mid-purge is unreadable the moment the key
// is gone, which is why the key is destroyed in the same transaction as the
// deletions rather than after them.
func (r *Runner) Purge(ctx context.Context) (int, error) {
	cutoff := r.now().Add(-r.opts.PurgeGrace)

	candidates, err := pg.PurgeableIdentities(ctx, r.db, cutoff, r.opts.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("lifecycle: finding purgeable identities: %w", err)
	}

	purged := 0
	for _, identityID := range candidates {
		done, err := r.purgeOne(ctx, identityID)
		if err != nil {
			// One identity failing must not stop the rest: a single bad row
			// would otherwise hold up every purge behind it forever.
			slog.Error("purging identity", "identity_id", identityID, "error", err)
			continue
		}
		if done {
			purged++
		}
	}
	if purged > 0 {
		slog.Info("purged identities", "count", purged)
	}
	return purged, nil
}

func (r *Runner) purgeOne(ctx context.Context, identityID core.UUID) (bool, error) {
	reservedUntil := r.now().Add(r.opts.ReservePeriod)

	var (
		blobIDs []core.UUID
		purged  bool
	)
	err := r.db.InTx(ctx, func(q pg.Querier) error {
		ids, err := pg.IdentityBlobs(ctx, q, identityID)
		if err != nil {
			return err
		}
		blobIDs = ids

		if _, err := pg.DeleteIdentityDeliveries(ctx, q, identityID); err != nil {
			return err
		}
		for _, blobID := range blobIDs {
			if _, err := pg.ReleaseBlob(ctx, q, blobID); err != nil {
				return err
			}
		}

		// The guard is inside this statement, so a concurrent purge of the
		// same identity moves no row and emits no second event.
		moved, err := pg.DestroyDataKey(ctx, q, identityID, reservedUntil)
		if err != nil {
			return err
		}
		purged = moved
		if !moved {
			return nil
		}
		_, err = pg.AppendEvent(ctx, q, &identityID, core.EventIdentityPurged,
			map[string]any{"reserved_until": reservedUntil})
		return err
	})
	if err != nil {
		return false, err
	}
	if purged {
		r.collectBlobs(ctx, blobIDs)
	}
	return purged, nil
}

// Sweep applies the rolling retention of named inboxes.
//
// A named inbox is permanent but its messages are not, and it is shared, so the
// sweep bounds it two ways: by age, and by count. One noisy sender must not be
// able to push everybody else's mail out by volume alone.
func (r *Runner) Sweep(ctx context.Context) (int, error) {
	identities, err := pg.SweptNamedIdentities(ctx, r.db, defaultNamedBatch)
	if err != nil {
		return 0, fmt.Errorf("lifecycle: finding named inboxes: %w", err)
	}

	swept := 0
	for i := range identities {
		n, err := r.sweepOne(ctx, &identities[i])
		if err != nil {
			slog.Error("sweeping named inbox", "identity_id", identities[i].ID, "error", err)
			continue
		}
		swept += n
	}
	if swept > 0 {
		slog.Info("swept expired messages", "count", swept)
	}
	return swept, nil
}

func (r *Runner) sweepOne(ctx context.Context, identity *core.Identity) (int, error) {
	retention := r.opts.NamedRetention
	if identity.RetentionHours != nil && *identity.RetentionHours > 0 {
		retention = time.Duration(*identity.RetentionHours) * time.Hour
	}
	olderThan := r.now().Add(-retention)

	expired, err := pg.ExpiredDeliveries(ctx, r.db, identity.ID, olderThan, identity.QuotaMessages)
	if err != nil {
		return 0, err
	}
	if len(expired) == 0 {
		return 0, nil
	}
	if len(expired) > defaultRetentionMax {
		expired = expired[:defaultRetentionMax]
	}

	var (
		blobIDs []core.UUID
		removed int
	)
	err = r.db.InTx(ctx, func(q pg.Querier) error {
		blobIDs, removed = nil, 0
		for i := range expired {
			delivery := &expired[i]
			ids, err := pg.DeliveryBlobs(ctx, q, delivery.ID)
			if err != nil {
				return err
			}
			deleted, err := pg.DeleteDelivery(ctx, q, delivery.ID)
			if err != nil {
				return err
			}
			if !deleted {
				// Another sweep got there first.
				continue
			}
			for _, blobID := range ids {
				if _, err := pg.ReleaseBlob(ctx, q, blobID); err != nil {
					return err
				}
			}
			blobIDs = append(blobIDs, ids...)
			removed++

			if _, err := pg.AppendEvent(ctx, q, &identity.ID, core.EventMessageExpired,
				map[string]any{"delivery_id": delivery.ID, "seq": delivery.Seq}); err != nil {
				return err
			}
		}

		// The identity's usage has to come down with its messages, or a swept
		// inbox stays permanently full.
		if removed > 0 {
			var freed int64
			for i := range expired[:removed] {
				freed += expired[i].SizeBytes
			}
			if _, err := q.Exec(ctx, `
				UPDATE identities
				   SET used_messages = GREATEST(used_messages - $2, 0),
				       used_bytes    = GREATEST(used_bytes - $3, 0)
				 WHERE id = $1`, identity.ID, removed, freed); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	r.collectBlobs(ctx, blobIDs)
	return removed, nil
}

// Release reports on the tombstones.
//
// It deletes nothing, on purpose. A reserved identity past its reservation
// period stays in the table forever, because the row is the only thing that
// keeps the address permanently unallocatable. The job exists so that the
// decision is visible and counted rather than merely implied by the absence of
// a cleanup job.
func (r *Runner) Release(ctx context.Context) (int64, error) {
	count, err := pg.CountReservedIdentities(ctx, r.db, r.now())
	if err != nil {
		return 0, fmt.Errorf("lifecycle: counting reserved identities: %w", err)
	}
	slog.Info("reserved addresses held permanently", "count", count)
	return count, nil
}

// collectBlobs removes blobs nothing references any more.
//
// The row goes first and the bytes second. A row pointing at bytes that are
// gone is an unreadable message; bytes with no row are only wasted space, and a
// later delivery of the same content writes them again anyway. There remains a
// narrow window where a delivery of identical content lands between the row
// being deleted and the bytes being removed; closing it entirely needs an
// advisory lock keyed on the content hash across the ingestion path, which is
// more coupling than the failure is worth at this size.
func (r *Runner) collectBlobs(ctx context.Context, blobIDs []core.UUID) {
	seen := make(map[core.UUID]bool, len(blobIDs))
	for _, blobID := range blobIDs {
		if seen[blobID] {
			continue
		}
		seen[blobID] = true

		var (
			sha     []byte
			claimed bool
		)
		err := r.db.InTx(ctx, func(q pg.Querier) error {
			var err error
			sha, _, claimed, err = pg.ClaimOrphanBlob(ctx, q, blobID)
			return err
		})
		if err != nil {
			slog.Error("claiming an orphaned blob", "blob_id", blobID, "error", err)
			continue
		}
		if !claimed {
			continue
		}

		address, err := blob.SHA256FromBytes(sha)
		if err != nil {
			slog.Error("orphaned blob has an unreadable address", "blob_id", blobID, "error", err)
			continue
		}
		if err := r.blobs.Delete(ctx, address); err != nil && !errors.Is(err, blob.ErrNotFound) {
			slog.Error("deleting orphaned blob bytes", "blob_id", blobID, "error", err)
		}
	}
}

// CollectOrphans sweeps blobs left unreferenced by anything, including ones
// orphaned by a failure part way through an earlier job.
func (r *Runner) CollectOrphans(ctx context.Context) (int, error) {
	orphans, err := pg.OrphanedBlobs(ctx, r.db, defaultBlobBatch)
	if err != nil {
		return 0, fmt.Errorf("lifecycle: finding orphaned blobs: %w", err)
	}
	ids := make([]core.UUID, 0, len(orphans))
	for i := range orphans {
		ids = append(ids, orphans[i].ID)
	}
	r.collectBlobs(ctx, ids)
	return len(ids), nil
}
