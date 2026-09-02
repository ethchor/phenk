package web

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// built reports whether this binary carries a frontend build. Without one the
// handler tests have nothing to serve, and skipping is honest: `go test` on a
// fresh checkout should not fail because nobody has run `make web`.
func built(t *testing.T) fs.FS {
	t.Helper()
	files, err := FS()
	if errors.Is(err, ErrNotBuilt) {
		t.Skip("the frontend has not been built; run `make web`")
	}
	if err != nil {
		t.Fatalf("FS: %v", err)
	}
	return files
}

func TestServesTheApp(t *testing.T) {
	built(t)
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	response := get(t, handler, "/")
	if response.Code != http.StatusOK {
		t.Fatalf("status %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(response.Body.String(), "<div id=\"root\">") {
		t.Fatalf("the served page is not the app: %s", response.Body.String()[:200])
	}
}

func TestSinglePageFallback(t *testing.T) {
	// A deep link and a refresh both have to land on the app rather than a 404,
	// because the client owns routing.
	built(t)
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	for _, path := range []string{"/inbox", "/some/deep/route", "/inbox?x=1"} {
		response := get(t, handler, path)
		if response.Code != http.StatusOK {
			t.Errorf("%s returned %d, want the app", path, response.Code)
		}
		if !strings.Contains(response.Body.String(), "<div id=\"root\">") {
			t.Errorf("%s did not return the app", path)
		}
	}
}

func TestAssetsAreCachedAndTheShellIsNot(t *testing.T) {
	// Asset names carry a content hash, so they can be cached forever; the
	// shell points at those hashes and so can never be.
	files := built(t)
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var asset string
	_ = fs.WalkDir(files, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && asset == "" {
			asset = path
		}
		return nil
	})
	if asset == "" {
		t.Skip("the build produced no hashed assets")
	}

	response := get(t, handler, "/"+asset)
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned %d", asset, response.Code)
	}
	if got := response.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("asset cache-control = %q, want immutable", got)
	}

	shell := get(t, handler, "/")
	if got := shell.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("shell cache-control = %q, want no-cache", got)
	}
}

func TestShellCarriesFramingProtections(t *testing.T) {
	built(t)
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	response := get(t, handler, "/")
	for header, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestPathTraversalCannotEscapeTheEmbeddedFiles(t *testing.T) {
	built(t)
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	for _, path := range []string{"/../go.mod", "/../../etc/passwd", "/assets/../../go.sum"} {
		response := get(t, handler, path)
		body := response.Body.String()
		if strings.Contains(body, "module github.com/ethchor/phenk") {
			t.Fatalf("%s escaped the embedded filesystem", path)
		}
	}
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
