package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethchor/phenk/internal/alloc"
	"github.com/ethchor/phenk/internal/core"
	"github.com/ethchor/phenk/internal/crypto"
	"github.com/ethchor/phenk/internal/events"
	"github.com/ethchor/phenk/internal/store/blob"
	"github.com/ethchor/phenk/internal/store/pg"
	"github.com/ethchor/phenk/internal/testsupport/pgtest"
)

var testDB *pg.DB

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	dsn, err := pgtest.DatabaseFor(setupCtx, "api")
	cancelSetup()
	if err == nil {
		var db *pg.DB
		openCtx, cancelOpen := context.WithTimeout(context.Background(), 15*time.Second)
		db, err = pg.Open(openCtx, dsn, 16)
		cancelOpen()
		if err == nil {
			defer db.Close()
			migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 60*time.Second)
			err = db.Migrate(migrateCtx)
			cancelMigrate()
			if err == nil {
				testDB = db
				os.Exit(m.Run())
			}
		}
	}

	if pgtest.Required() {
		panic("PHENK_TEST_REQUIRED is set but the test database is unusable: " + err.Error())
	}
	os.Stderr.WriteString("api: skipping database tests: " + err.Error() + "\n")
	os.Exit(m.Run())
}

// harness is a running API server over the real storage layer.
type harness struct {
	t         *testing.T
	db        *pg.DB
	blobs     *blob.FS
	keyring   *crypto.Keyring
	allocator *alloc.Allocator
	hub       *events.Hub
	server    *Server
	http      *httptest.Server

	randomDomain core.Domain
	publicDomain core.Domain
}

func newHarness(t *testing.T, configure ...func(*Config)) *harness {
	t.Helper()
	if testDB == nil {
		t.Skip("no test database")
	}
	ctx := context.Background()

	if _, err := testDB.Exec(ctx, `
		TRUNCATE attachments, parsed_messages, deliveries, events, identities, blobs, domains RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset: %v", err)
	}

	master, err := crypto.ParseMasterKey(crypto.GenerateMasterKey())
	if err != nil {
		t.Fatalf("ParseMasterKey: %v", err)
	}
	keyring, err := crypto.NewKeyring(master)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	blobs, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	h := &harness{t: t, db: testDB, blobs: blobs, keyring: keyring}
	h.randomDomain = h.addDomain("rand.test", core.PoolRandom)
	h.publicDomain = h.addDomain("pub.test", core.PoolPublic)
	h.allocator = alloc.New(keyring, alloc.Options{
		DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour, NamedRetention: 168 * time.Hour,
	})

	hubCtx, cancelHub := context.WithCancel(ctx)
	h.hub = events.NewHub(testDB)
	go func() { _ = h.hub.Run(hubCtx) }()
	select {
	case <-h.hub.Ready():
	case <-time.After(10 * time.Second):
		cancelHub()
		t.Fatal("the event hub never started listening")
	}
	t.Cleanup(cancelHub)

	cfg := Config{
		CookieName:     "phenk_session",
		CookieSecure:   false,
		MaxWaitTimeout: 5 * time.Second,
		DefaultTTL:     time.Hour,
		MaxTTL:         24 * time.Hour,
		NamedPerIPHour: 1000,
		Version:        "test",
	}
	for _, fn := range configure {
		fn(&cfg)
	}

	h.server = New(cfg, testDB, blobs, keyring, h.allocator, h.hub)
	h.http = httptest.NewServer(h.server.Handler())
	t.Cleanup(h.http.Close)
	return h
}

func (h *harness) addDomain(name string, pool core.Pool) core.Domain {
	h.t.Helper()
	d := &core.Domain{Name: name, State: core.DomainActive, Pool: pool}
	if err := pg.CreateDomain(context.Background(), h.db, d); err != nil {
		h.t.Fatalf("CreateDomain(%s): %v", name, err)
	}
	return *d
}

// client is an HTTP client that keeps the session cookie, like a browser.
type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (h *harness) client() *client {
	h.t.Helper()
	jar := &cookieJar{}
	return &client{t: h.t, base: h.http.URL, http: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
}

// anonymous is a client with no cookie jar, standing in for a stranger.
func (h *harness) anonymous() *client {
	h.t.Helper()
	return &client{t: h.t, base: h.http.URL, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *client) do(method, path string, body any) *http.Response {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("encoding request: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("building request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return response
}

// decode reads a JSON response, failing the test on an unexpected status.
func (c *client) decode(response *http.Response, wantStatus int, into any) {
	c.t.Helper()
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatalf("reading response: %v", err)
	}
	if response.StatusCode != wantStatus {
		c.t.Fatalf("status %d, want %d: %s", response.StatusCode, wantStatus, body)
	}
	if into != nil {
		if err := json.Unmarshal(body, into); err != nil {
			c.t.Fatalf("decoding response %s: %v", body, err)
		}
	}
}

// cookieJar is a minimal jar: the tests only ever talk to one host.
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	j.cookies = append(j.cookies, cookies...)
}
func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie { return j.cookies }
