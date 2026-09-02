package pg

import (
	"context"
	"errors"
	"testing"
)

func TestAcquireAndReleaseBlob(t *testing.T) {
	reset(t)
	ctx := context.Background()
	sha := testSHA("some raw mime")

	id, inserted, err := AcquireBlob(ctx, testDB, sha, 13, sha.String())
	if err != nil || !inserted {
		t.Fatalf("first AcquireBlob: inserted=%v err=%v", inserted, err)
	}
	again, inserted, err := AcquireBlob(ctx, testDB, sha, 13, sha.String())
	if err != nil {
		t.Fatalf("second AcquireBlob: %v", err)
	}
	if inserted {
		t.Fatal("the second acquire reported an insert")
	}
	if again != id {
		t.Fatal("the same content produced two blob ids")
	}

	row, err := BlobByID(ctx, testDB, id)
	if err != nil {
		t.Fatalf("BlobByID: %v", err)
	}
	if row.Refcount != 2 || row.SizeBytes != 13 {
		t.Fatalf("got %+v", row)
	}

	if n, err := ReleaseBlob(ctx, testDB, id); err != nil || n != 1 {
		t.Fatalf("ReleaseBlob: %d %v", n, err)
	}
	if n, err := ReleaseBlob(ctx, testDB, id); err != nil || n != 0 {
		t.Fatalf("ReleaseBlob: %d %v", n, err)
	}
	// Never negative: an over-release is clamped rather than violating the
	// CHECK constraint and aborting a purge.
	if n, err := ReleaseBlob(ctx, testDB, id); err != nil || n != 0 {
		t.Fatalf("over-release: %d %v", n, err)
	}
}

func TestOrphanedBlobsAndCollection(t *testing.T) {
	reset(t)
	ctx := context.Background()

	kept := testSHA("still referenced")
	orphan := testSHA("nothing points here")
	keptID, _, err := AcquireBlob(ctx, testDB, kept, 16, kept.String())
	if err != nil {
		t.Fatalf("AcquireBlob: %v", err)
	}
	orphanID, _, err := AcquireBlob(ctx, testDB, orphan, 19, orphan.String())
	if err != nil {
		t.Fatalf("AcquireBlob: %v", err)
	}
	if _, err := ReleaseBlob(ctx, testDB, orphanID); err != nil {
		t.Fatalf("ReleaseBlob: %v", err)
	}

	orphans, err := OrphanedBlobs(ctx, testDB, 10)
	if err != nil {
		t.Fatalf("OrphanedBlobs: %v", err)
	}
	if len(orphans) != 1 || orphans[0].ID != orphanID {
		t.Fatalf("got %d orphans, want just the released one", len(orphans))
	}

	deleted, err := DeleteBlobRow(ctx, testDB, orphanID)
	if err != nil || !deleted {
		t.Fatalf("DeleteBlobRow: %v %v", deleted, err)
	}
	if _, err := BlobByID(ctx, testDB, orphanID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("orphan survived collection: %v", err)
	}

	// A blob that gained a reference between the scan and the delete is kept:
	// the collector loses that race harmlessly.
	deleted, err = DeleteBlobRow(ctx, testDB, keptID)
	if err != nil {
		t.Fatalf("DeleteBlobRow: %v", err)
	}
	if deleted {
		t.Fatal("collected a blob that was still referenced")
	}
}
