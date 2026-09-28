# phenk

Public email inboxes for people and agents. Type any name and read the mail
sent to it — no account, no password, nothing to confirm.

Mail to `anything@<your public domain>` lands in the inbox called `anything`,
and anyone who knows the name can read it. Messages are kept for a rolling
window, seven days by default, then deleted.

## What makes it different

**Any name is an inbox.** Nothing to create first: type a name, or use it in a
form and read it later. The app says plainly, wherever an inbox is shown, that
anyone who knows the name can read it.

**Built for agents and test suites.** One request takes an agent from "wait for
mail" to "use the code": waiting on a name nobody has used opens its inbox, and
the answer comes back once the message is parsed, with verification codes and
account links already detected. `/llms.txt` explains the whole flow to a model
in one page.

```sh
curl -s "https://app.example/v1/named/agent-7f3c9a21/wait?timeout=60"
# .messages[0].extracted.codes[0].value  →  "482913"
```

**Codes are detected conservatively.** A wrong code is worse than none, so a
candidate only counts when the words around it say it is a code; amounts,
phone numbers, years, dates and URL fragments are thrown away. Every detected
code is shown with the line it came from.

**Nothing is accepted that cannot be delivered.** An unknown or refused
recipient is rejected at `RCPT TO` with a `550`, never accepted and dropped. A
message is never acknowledged with `250` until it is durably committed.

**Message HTML is treated as hostile.** It is stripped of everything executable
on the way in, before it is encrypted and stored, and then rendered in an iframe
with no scripting and no same-origin access. Remote images are fetched by the
server, so a sender never learns the reader's address or when they opened it.

**Encrypted at rest, per inbox.** Every inbox has its own key and everything it
receives is encrypted under it.

**Designed to Apple's Human Interface Guidelines.** The inbox app follows the
current HIG on the web — Liquid Glass only for navigation, system colours and
type, Dark Mode from the system, keyboard shortcuts, and layouts that adapt by
width. [docs/design.md](docs/design.md) records the guidance and the decisions.

**Private addresses, if you want them.** With `PHENK_FEATURE_DISPOSABLE=true`,
Phenk also hands out private, unguessable addresses that belong to one browser
and are destroyed on a deadline. Expiry destroys the address's key, so its mail
stops being readable — by the operator too — and a destroyed address is never
reused. Off by default.

## Running it

You need Go 1.24+, Node 22+, and PostgreSQL 16.

```sh
# Start Postgres.
make dev-db

# Generate a master key and put it in the environment. Losing it destroys every
# inbox, which is the point of it.
export PHENK_MASTER_KEY="$(go run ./cmd/phenk genkey)"
export PHENK_DATABASE_URL="postgres://phenk:phenk@localhost:5432/phenk?sslmode=disable"

# Build the inbox app into the binary and compile.
make build

# Add a public domain for inboxes to live on, and activate it.
./bin/phenk domain add phenk.test public active

# Optional: private addresses need a random-pool domain and the feature flag.
#   ./bin/phenk domain add private.test random active
#   export PHENK_FEATURE_DISPOSABLE=true

# Run everything in one process.
./bin/phenk all
```

The inbox is then at http://localhost:8080 and the SMTP listener on port 25.
For development the frontend runs separately with hot reload:

```sh
npm run dev   # Vite on :5173, proxying the API to :8080
```

### Run modes

One binary, several modes. A self-hoster runs `all`; a fleet operator runs them
separately so a burst of inbound mail cannot starve the API.

| | |
|---|---|
| `phenk smtpd` | accept inbound mail |
| `phenk api` | serve the HTTP API and the inbox app |
| `phenk worker` | run parse and lifecycle jobs |
| `phenk all` | all three in one process |
| `phenk migrate` | apply migrations and exit |
| `phenk genkey` | print a new master key |
| `phenk domain` | list, add, or change the state of a domain |

Configuration is entirely environment variables. `PHENK_DATABASE_URL` and
`PHENK_MASTER_KEY` are required; everything else has a working default. See
`internal/config/config.go`, which is the list.

## Before you point real mail at it

Read [docs/phase-0-infrastructure.md](docs/phase-0-infrastructure.md) first. It
covers the DNS records, the TLS certificate, and — most importantly — confirming
that your host actually permits **inbound** connections on port 25. Many
providers block it silently, and finding that out after building on them is an
expensive way to learn it.

`tools/smtpsink` is there for exactly that check.

## Development

```sh
make preflight   # the only gate that matters
```

`scripts/preflight.py` reads `.github/workflows/ci.yml` and runs each of its
steps locally, in order, with the same environment. There is no second copy of
the build to drift from: a step added to CI is a step preflight runs, and it
refuses to claim parity if CI grows a step it cannot execute.

It also builds a pristine archive of `HEAD`, which catches the one thing every
other check misses — a file that exists on your machine but was never committed.

Other targets:

```sh
make test        # the Go suite
make web         # build the inbox app into internal/web/dist
make site        # build the marketing site
make generate    # regenerate API stubs from api/openapi.yaml
```

The storage tests need Postgres. Without it they skip, so `go test ./...` stays
green on a machine that has none; CI sets `PHENK_TEST_REQUIRED=1` so a missing
database is a failure there rather than a silent gap in coverage.

## Layout

```
cmd/phenk/          the binary
internal/
  core/             domain types, with no infrastructure imports
  crypto/           master key, per-identity data keys, streamed encryption
  store/pg/         queries and migrations
  store/blob/       content-addressed blob storage
  smtpd/            the inbound listener
  mimeparse/        MIME to structured output
  sanitize/         HTML sanitizing and the image proxy rewrite
  events/           the LISTEN/NOTIFY hub
  api/              HTTP handlers, SSE, long-poll wait, llms.txt
  extract/          verification code and account link detection
  worker/parse/     the parse job
  worker/lifecycle/ expire, purge, retention
  web/              go:embed of the built inbox app
api/openapi.yaml    the contract: server stubs and client types both come from it
packages/ui/        theme and components shared by both frontends
web/                the inbox app (Vite + React), embedded in the binary
site/               the marketing site (Next.js), deployed separately
testdata/mime/      golden fixtures, as real byte-exact .eml files
docs/               design notes worth keeping
```

## Design notes

- [docs/design.md](docs/design.md) — how the inbox app applies Apple's Human
  Interface Guidelines, the values taken from them, and the decisions that
  followed.
- [docs/deployment.md](docs/deployment.md) — choosing a host, the DNS record
  set, and why Cloudflare is the right place for DNS and the wrong place for
  the mail server.
- [docs/blob-encryption.md](docs/blob-encryption.md) — why raw messages use
  envelope keys, and the conflict between two of the project's own invariants
  that forced the choice.
- [docs/phase-0-infrastructure.md](docs/phase-0-infrastructure.md) — the
  infrastructure checklist, and what to do if port 25 is blocked.

## Status

The ingestion, parsing, API, agent workflow, lifecycle and inbox surfaces are
built and tested, and the inbox app has been exercised in a real browser at
phone, tablet and desktop widths. What remains before this receives real mail is
the infrastructure proof: a registered domain, MX records, and a host that
accepts inbound port 25.
