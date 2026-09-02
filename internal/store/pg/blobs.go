package pg

import (
	"context"

	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/store/blob"
)

// AcquireBlob records a reference to a blob, inserting the row the first time
// the content is seen and bumping the refcount every time after. It returns the
// blob id and whether this call created the row.
//
// The same message delivered to two identities takes this path twice and
// produces one blob row with a refcount of two, which is the whole point of
// content addressing.
func AcquireBlob(ctx context.Context, q Querier, sha blob.SHA256, size int64, path string) (core.UUID, bool, error) {
	var (
		id       core.UUID
		inserted bool
	)
	err := q.QueryRow(ctx, `
		INSERT INTO blobs (id, sha256, size_bytes, path, refcount)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (sha256) DO UPDATE SET refcount = blobs.refcount + 1
		RETURNING id, (xmax = 0)`,
		core.NewUUID(), sha.Bytes(), size, path).Scan(&id, &inserted)
	if err != nil {
		return core.NilUUID, false, mapError(err)
	}
	return id, inserted, nil
}

// ReleaseBlob drops one reference and returns the remaining count. A blob at
// zero is an orphan: its bytes are deleted from the blob store and its row
// collected by the same job that dropped the reference.
func ReleaseBlob(ctx context.Context, q Querier, id core.UUID) (int, error) {
	var refcount int
	err := q.QueryRow(ctx,
		`UPDATE blobs SET refcount = GREATEST(refcount - 1, 0) WHERE id = $1 RETURNING refcount`,
		id).Scan(&refcount)
	if err != nil {
		return 0, mapError(err)
	}
	return refcount, nil
}

// BlobByID reads a blob row.
func BlobByID(ctx context.Context, q Querier, id core.UUID) (*core.Blob, error) {
	var b core.Blob
	err := q.QueryRow(ctx,
		`SELECT id, sha256, size_bytes, path, refcount, created_at FROM blobs WHERE id = $1`, id).
		Scan(&b.ID, &b.SHA256, &b.SizeBytes, &b.Path, &b.Refcount, &b.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &b, nil
}

// OrphanedBlobs lists blobs no longer referenced by anything, oldest first, so
// a collector can delete their bytes and then their rows.
func OrphanedBlobs(ctx context.Context, q Querier, limit int) ([]core.Blob, error) {
	rows, err := q.Query(ctx, `
		SELECT id, sha256, size_bytes, path, refcount, created_at
		  FROM blobs WHERE refcount = 0 ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Blob
	for rows.Next() {
		var b core.Blob
		if err := rows.Scan(&b.ID, &b.SHA256, &b.SizeBytes, &b.Path, &b.Refcount, &b.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, b)
	}
	return out, mapError(rows.Err())
}

// DeleteBlobRow removes a blob row, but only while it is still unreferenced: a
// delivery that arrived between the orphan scan and this call re-acquires the
// blob, and the guard means the collector loses that race harmlessly.
func DeleteBlobRow(ctx context.Context, q Querier, id core.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM blobs WHERE id = $1 AND refcount = 0`, id)
	if err != nil {
		return false, mapError(err)
	}
	return tag.RowsAffected() > 0, nil
}
