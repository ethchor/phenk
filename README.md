# phenk

A privacy-first temporary email platform, built for humans, developers, and
autonomous agents.

Take an address, receive real mail on it in real time, and let it destroy itself
on a deadline. No account, no password, and no forwarding address that outlives
its purpose.

## What makes it different

**Expiry is real.** Every address has its own encryption key and everything it
receives is encrypted under it. Expiry destroys that key. The messages are not
hidden or flagged deleted; they stop being readable, including by the operator.

**Nothing is accepted that cannot be delivered.** An unknown, expired or purged
recipient is rejected at `RCPT TO` with a `550`, never accepted and dropped. A
message is never acknowledged with `250` until it is durably committed.

**Addresses are never reused.** A destroyed address stays in the table as a
tombstone forever, so mail for it can never reach somebody else.

**Message HTML is treated as hostile.** It is stripped of everything executable
on the way in, before it is encrypted and stored, and then rendered in an iframe
with no scripting and no same-origin access. Remote images are fetched by the
server, so a sender never learns the reader's address or when they opened it.

**Public inboxes are labelled as public.** You can open any inbox by typing its
name. Every surface that shows one says plainly that anyone who guesses the name
can read it, and named inboxes can never hold an API key, a webhook, or a grant.

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

# Add a domain to hand out addresses on, and activate it.
./bin/phenk domain add phenk.test random active
./bin/phenk domain add public.test public active

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
  api/              HTTP handlers, SSE, long-poll wait
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

- [docs/blob-encryption.md](docs/blob-encryption.md) — why raw messages use
  envelope keys, and the conflict between two of the project's own invariants
  that forced the choice.
- [docs/phase-0-infrastructure.md](docs/phase-0-infrastructure.md) — the
  infrastructure checklist, and what to do if port 25 is blocked.

## Status

The ingestion, parsing, API, lifecycle and inbox surfaces are built and tested.
What remains before this receives real mail is the infrastructure proof: a
registered domain, MX records, and a host that accepts inbound port 25.
