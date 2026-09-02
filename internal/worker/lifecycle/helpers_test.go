package lifecycle

import (
	"io"
	"io/fs"
	"net/netip"
	"path/filepath"
	"strings"
)

func mustAddr(s string) netip.Addr { return netip.MustParseAddr(s) }

func filepathWalk(root string, visit func(isDir bool)) error {
	return filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visit(entry.IsDir())
		return nil
	})
}

func bytesReader(s string) io.Reader { return strings.NewReader(s) }
