package pg

import (
	"context"
	"time"

	"github.com/ethchor/phenk/internal/core"
)

// ExpireDueIdentities moves identities past their deadline to expired and
// returns the ones that moved.
//
// The state predicate is in the WHERE clause, which is what makes the job
// idempotent: a second run matches nothing and changes nothing. Named
// identities have no expires_at at all, so they are excluded by the comparison
// rather than by a special case.
func ExpireDueIdentities(ctx context.Context, q Querier, now time.Time, limit int) ([]core.UUID, error) {
	return updateReturningIDs(ctx, q, `
		UPDATE identities SET state = 'expired'
		 WHERE id IN (
		   SELECT id FROM identities
		    WHERE state IN ('active', 'expiring') AND expires_at IS NOT NULL AND expires_at < $1
		    ORDER BY expires_at
		    LIMIT $2
		    FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id`, now, limit)
}

// MarkExpiringIdentities moves identities within the notice window to expiring
// and returns the ones that moved, so a notice is emitted exactly once.
func MarkExpiringIdentities(ctx context.Context, q Querier, threshold time.Time, limit int) ([]core.UUID, error) {
	return updateReturningIDs(ctx, q, `
		UPDATE identities SET state = 'expiring'
		 WHERE id IN (
		   SELECT id FROM identities
		    WHERE state = 'active' AND expires_at IS NOT NULL AND expires_at < $1
		    ORDER BY expires_at
		    LIMIT $2
		    FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id`, threshold, limit)
}

// PurgeableIdentities lists expired identities past the grace window that still
// hold a data key. An identity whose key is already destroyed has been purged,
// so it is not returned again.
func PurgeableIdentities(ctx context.Context, q Querier, before time.Time, limit int) ([]core.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT id FROM identities
		 WHERE state = 'expired'
		   AND expires_at IS NOT NULL AND expires_at < $1
		   AND octet_length(wrapped_data_key) > 0
		 ORDER BY expires_at
		 LIMIT $2`, before, limit)
	if err != nil {
		return nil, mapError(err)
	}
	return collectIDs(rows)
}

// SweptNamedIdentities lists named identities that may have messages to sweep.
func SweptNamedIdentities(ctx context.Context, q Querier, limit int) ([]core.Identity, error) {
	rows, err := q.Query(ctx,
		`SELECT `+identityColumns+`
		   FROM identities
		  WHERE kind = 'named' AND state = 'active' AND used_messages > 0
		  ORDER BY created_at
		  LIMIT $1`, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Identity
	for rows.Next() {
		identity, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *identity)
	}
	return out, mapError(rows.Err())
}

// ExpiredDeliveries lists an identity's deliveries that have outlived its
// rolling retention window, plus any beyond its message quota, oldest first.
//
// The quota is swept as well as the age because a named inbox is shared: one
// noisy sender must not be able to push everyone else's mail out by volume
// alone, and the oldest is the least likely to still be wanted.
func ExpiredDeliveries(ctx context.Context, q Querier, identityID core.UUID, olderThan time.Time, keepNewest int) ([]core.Delivery, error) {
	rows, err := q.Query(ctx, `
		SELECT `+deliveryColumns+` FROM deliveries
		 WHERE identity_id = $1
		   AND (received_at < $2 OR seq <= (
		     SELECT COALESCE(max(seq), 0) - $3 FROM deliveries WHERE identity_id = $1
		   ))
		 ORDER BY seq`, identityID, olderThan, keepNewest)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, mapError(rows.Err())
}

// DeliveryBlobs lists every blob a delivery references: the raw message and any
// attachment bodies. Releasing them is how storage is actually reclaimed.
func DeliveryBlobs(ctx context.Context, q Querier, deliveryID core.UUID) ([]core.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT blob_id FROM deliveries WHERE id = $1
		UNION ALL
		SELECT blob_id FROM attachments WHERE delivery_id = $1 AND blob_id IS NOT NULL`, deliveryID)
	if err != nil {
		return nil, mapError(err)
	}
	return collectIDs(rows)
}

// IdentityBlobs lists every blob an identity's messages reference.
func IdentityBlobs(ctx context.Context, q Querier, identityID core.UUID) ([]core.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT d.blob_id FROM deliveries d WHERE d.identity_id = $1
		UNION ALL
		SELECT a.blob_id FROM attachments a
		  JOIN deliveries d ON d.id = a.delivery_id
		 WHERE d.identity_id = $1 AND a.blob_id IS NOT NULL`, identityID)
	if err != nil {
		return nil, mapError(err)
	}
	return collectIDs(rows)
}

// DeleteDelivery removes a delivery and, by cascade, its parsed output and
// attachment rows. The caller releases the blobs it referenced first.
func DeleteDelivery(ctx context.Context, q Querier, deliveryID core.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM deliveries WHERE id = $1`, deliveryID)
	if err != nil {
		return false, mapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteIdentityDeliveries removes every delivery an identity holds.
func DeleteIdentityDeliveries(ctx context.Context, q Querier, identityID core.UUID) (int64, error) {
	tag, err := q.Exec(ctx, `DELETE FROM deliveries WHERE identity_id = $1`, identityID)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// ClaimOrphanBlob locks an unreferenced blob and deletes its row, returning the
// content address whose bytes the caller should now remove.
//
// The row is locked and its refcount re-checked inside the transaction, so a
// delivery that acquires the blob between the scan and this call keeps it. The
// bytes are deleted after the row, never before: a row pointing at bytes that
// are gone is an unreadable message, while bytes with no row are only wasted
// space that a later delivery of the same content reuses.
func ClaimOrphanBlob(ctx context.Context, q Querier, blobID core.UUID) ([]byte, string, bool, error) {
	var (
		sha  []byte
		path string
	)
	err := q.QueryRow(ctx, `
		DELETE FROM blobs
		 WHERE id = (SELECT id FROM blobs WHERE id = $1 AND refcount = 0 FOR UPDATE)
		 RETURNING sha256, path`, blobID).Scan(&sha, &path)
	if err != nil {
		if mapped := mapError(err); mapped == ErrNotFound {
			return nil, "", false, nil
		}
		return nil, "", false, mapError(err)
	}
	return sha, path, true, nil
}

// CountReservedIdentities reports how many tombstones exist. Nothing ever
// deletes them: a reserved address stays in the table so it can never be handed
// to anyone else, and this is the number that says how many are held.
func CountReservedIdentities(ctx context.Context, q Querier, before time.Time) (int64, error) {
	var count int64
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM identities WHERE state = 'reserved' AND reserved_until < $1`, before).
		Scan(&count)
	return count, mapError(err)
}

func updateReturningIDs(ctx context.Context, q Querier, sql string, args ...any) ([]core.UUID, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapError(err)
	}
	return collectIDs(rows)
}

func collectIDs(rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
},
) ([]core.UUID, error) {
	defer rows.Close()
	var out []core.UUID
	for rows.Next() {
		var id core.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapError(err)
		}
		out = append(out, id)
	}
	return out, mapError(rows.Err())
}
