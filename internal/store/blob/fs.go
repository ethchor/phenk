package blob

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FS is a filesystem-backed Store. Blobs are sharded two levels deep by the
// first four hex characters of their address, which keeps any one directory to
// a manageable size without a rebalance step.
type FS struct {
	root string
}

// NewFS returns a filesystem store rooted at dir, creating it if needed.
func NewFS(dir string) (*FS, error) {
	if dir == "" {
		return nil, errors.New("blob: filesystem store needs a root directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("blob: resolving root: %w", err)
	}
	for _, d := range []string{abs, filepath.Join(abs, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("blob: creating %s: %w", d, err)
		}
	}
	return &FS{root: abs}, nil
}

// Locate implements Store.
func (f *FS) Locate(sha SHA256) string {
	h := sha.String()
	return filepath.Join(h[0:2], h[2:4], h)
}

func (f *FS) path(sha SHA256) string { return filepath.Join(f.root, f.Locate(sha)) }

// Put implements Store. It streams through a temporary file so nothing is ever
// buffered whole in memory, fsyncs the contents, then renames into place. A
// crash before the rename leaves a stray temp file and no blob, which is the
// safe direction: the delivery is not acknowledged either.
func (f *FS) Put(ctx context.Context, r io.Reader) (SHA256, int64, error) {
	var sha SHA256

	tmp, err := os.CreateTemp(filepath.Join(f.root, "tmp"), "put-*")
	if err != nil {
		return sha, 0, fmt.Errorf("blob: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), &contextReader{ctx: ctx, r: r})
	if err != nil {
		return sha, 0, fmt.Errorf("blob: writing: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return sha, 0, fmt.Errorf("blob: syncing: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return sha, 0, fmt.Errorf("blob: closing: %w", err)
	}
	copy(sha[:], h.Sum(nil))

	dest := f.path(sha)
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return sha, 0, fmt.Errorf("blob: creating shard: %w", err)
	}
	// Content addressing makes this idempotent: identical bytes hash to the
	// same name, so an existing file is already exactly what we would write.
	if _, err := os.Stat(dest); err == nil {
		return sha, size, nil
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return sha, 0, fmt.Errorf("blob: publishing: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return sha, 0, fmt.Errorf("blob: syncing shard: %w", err)
	}
	return sha, size, nil
}

// Get implements Store.
func (f *FS) Get(ctx context.Context, sha SHA256) (io.ReadCloser, error) {
	file, err := os.Open(f.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sha)
	}
	if err != nil {
		return nil, fmt.Errorf("blob: opening: %w", err)
	}
	return file, nil
}

// Delete implements Store.
func (f *FS) Delete(ctx context.Context, sha SHA256) error {
	err := os.Remove(f.path(sha))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blob: deleting: %w", err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// contextReader aborts a long copy when the caller's context is done, so a
// dropped SMTP session does not keep writing.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
