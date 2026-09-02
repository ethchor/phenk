package parse

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"time"

	"github.com/ethchor/phenk/internal/store/blob"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

func shaOf(b []byte) (blob.SHA256, error) { return blob.SHA256FromBytes(b) }

func readBlob(ctx context.Context, f *fixture, sha blob.SHA256) ([]byte, error) {
	rc, err := f.blobs.Get(ctx, sha)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func timeNowPlusYear() time.Time { return time.Now().Add(365 * 24 * time.Hour) }

// errRollback is any non-nil error, used to abort a transaction on purpose.
var errRollback = errors.New("deliberate rollback")
