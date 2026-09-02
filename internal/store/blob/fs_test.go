package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestFS(t *testing.T) *FS {
	t.Helper()
	fs, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return fs
}

func TestPutGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := newTestFS(t)
	payload := []byte("From: a@example.com\r\n\r\nbody")

	sha, size, err := fs.Put(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
	if want := sha256.Sum256(payload); sha != SHA256(want) {
		t.Fatal("returned address is not the sha256 of the content")
	}

	rc, err := fs.Get(ctx, sha)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Get = %q, want %q", got, payload)
	}
}

func TestPutIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fs := newTestFS(t)
	payload := []byte("the same message, delivered twice")

	a, sizeA, err := fs.Put(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	b, sizeB, err := fs.Put(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if a != b || sizeA != sizeB {
		t.Fatal("identical content produced different addresses")
	}

	// One message to two identities must be one file on disk.
	var files int
	root := filepath.Dir(filepath.Dir(filepath.Join(fs.root, fs.Locate(a))))
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			files++
		}
		return nil
	})
	if files != 1 {
		t.Fatalf("found %d files on disk, want 1", files)
	}
}

func TestPutLeavesNoTempFilesBehind(t *testing.T) {
	ctx := context.Background()
	fs := newTestFS(t)
	for i := 0; i < 3; i++ {
		if _, _, err := fs.Put(ctx, strings.NewReader("payload")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(fs.root, "tmp"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d temp files left behind", len(entries))
	}
}

func TestPutAbortsOnReadErrorWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	fs := newTestFS(t)
	r := io.MultiReader(strings.NewReader("partial"), errReader{})

	if _, _, err := fs.Put(ctx, r); err == nil {
		t.Fatal("Put succeeded despite a read error")
	}
	entries, _ := os.ReadDir(filepath.Join(fs.root, "tmp"))
	if len(entries) != 0 {
		t.Fatalf("%d temp files left behind after a failed Put", len(entries))
	}
	// Nothing may have been published: a half-written blob is exactly the
	// orphan state the SMTP commit path must never see.
	var published int
	_ = filepath.Walk(fs.root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.Contains(path, string(filepath.Separator)+"tmp"+string(filepath.Separator)) {
			published++
		}
		return nil
	})
	if published != 0 {
		t.Fatalf("%d blobs published after a failed Put", published)
	}
}

func TestPutHonoursContextCancellation(t *testing.T) {
	fs := newTestFS(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := fs.Put(ctx, strings.NewReader("payload")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestGetMissingReportsNotFound(t *testing.T) {
	fs := newTestFS(t)
	var sha SHA256
	if _, err := fs.Get(context.Background(), sha); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fs := newTestFS(t)
	sha, _, err := fs.Put(ctx, strings.NewReader("delete me"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := fs.Delete(ctx, sha); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := fs.Delete(ctx, sha); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if _, err := fs.Get(ctx, sha); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blob still readable after delete: %v", err)
	}
}

func TestLocateIsShardedAndStable(t *testing.T) {
	fs := newTestFS(t)
	sha := SHA256(sha256.Sum256([]byte("x")))
	got := fs.Locate(sha)
	h := sha.String()
	want := filepath.Join(h[0:2], h[2:4], h)
	if got != want {
		t.Fatalf("Locate = %q, want %q", got, want)
	}
}

func TestConcurrentPutsOfTheSameContent(t *testing.T) {
	ctx := context.Background()
	fs := newTestFS(t)
	payload := []byte("simultaneous identical delivery")

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := fs.Put(ctx, bytes.NewReader(payload)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Put: %v", err)
	}
}

func TestNewFSRejectsEmptyRoot(t *testing.T) {
	if _, err := NewFS(""); err == nil {
		t.Fatal("NewFS(\"\") succeeded")
	}
}

func TestSHA256Conversion(t *testing.T) {
	sha := SHA256(sha256.Sum256([]byte("x")))
	back, err := SHA256FromBytes(sha.Bytes())
	if err != nil {
		t.Fatalf("SHA256FromBytes: %v", err)
	}
	if back != sha {
		t.Fatal("round trip through bytes changed the address")
	}
	if _, err := SHA256FromBytes([]byte{1, 2, 3}); err == nil {
		t.Fatal("accepted a short address")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }
