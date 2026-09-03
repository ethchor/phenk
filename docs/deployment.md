# Deploying Phenk

Two things get deployed, to two different places, and the split is not
negotiable:

- **The binary** — SMTP listener, API, and worker — needs a host that accepts
  **inbound TCP on port 25**, has a static IP with editable reverse DNS, and can
  run a long-lived process alongside PostgreSQL.
- **The marketing site** — `site/` — is a Next.js app that goes on Vercel or
  Cloudflare Pages and never touches any of the above.

## What Cloudflare can and cannot do here

Cloudflare is the right place for DNS. It is not a place to run the mail server,
and the reasons are worth stating plainly so nobody rediscovers them under time
pressure.

**Workers cannot be an MX.** Cloudflare's own documentation is explicit: "it is
not possible to make an inbound TCP connection to your Worker." Support is
listed as coming; it is not here. Phenk is also a Go binary that needs
PostgreSQL and a filesystem, so it could not run there regardless.

**The proxy does not carry SMTP.** "Cloudflare does not proxy email traffic
(SMTP, port 25) by default." Every mail-related DNS record below must be set to
**DNS only** — the grey cloud, not the orange one. A proxied MX record does not
work.

**Spectrum can proxy port 25, and should not.** It is a genuine option and a bad
one for an MX. Cloudflare's documentation notes that the client IP becomes a
Cloudflare edge IP unless the origin implements PROXY protocol, which Phenk does
not; that Spectrum applications have no reverse DNS entries at all, which
senders check; and that mail may be rejected outright when an MX record points
at a Spectrum application. Losing the client IP would also make SPF
unevaluable, since SPF is defined over the connecting address.

**Email Routing is the fallback, not the plan.** If a host turns out to block
inbound 25 and cannot be moved, Cloudflare Email Routing can deliver to a Worker
which forwards to an HTTP ingest endpoint. The Worker receives the raw MIME and
the envelope addresses, so the message survives intact and DKIM still verifies —
but it receives no client IP and no TLS state, so `client_ip`, `helo` and `tls`
on the delivery could not be truthful and SPF could not be evaluated at all.
Phenk has no such ingest endpoint today. Treat this as the last resort
[docs/phase-0-infrastructure.md](phase-0-infrastructure.md) already describes.

## Choosing a host

The requirement is narrower than it first looks, because **Phenk never sends
mail**. It is a receive-only service. Almost every provider restriction written
about port 25 is on *outbound* traffic, as an anti-spam measure — DigitalOcean,
Vultr, Hetzner and AWS all block or throttle outbound 25 while leaving inbound
alone. That is not the constraint here.

What you actually need:

- inbound TCP 25 reachable from the internet
- a static IP
- **editable reverse DNS (PTR)** on that IP — some senders reject mail from
  hosts with no PTR, and this is the single most commonly missed requirement
- enough disk for the blob store, or R2 once the S3 backend lands

Do not take any list of providers on trust, this one included. Run the check:

```sh
go run ./tools/smtpsink -addr :25 -dir ./inbox -hostname mx1.yourdomain.example
```

Then send it mail from Gmail and from Outlook. Two `.eml` files on disk is the
only evidence that counts, and it takes ten minutes.

## DNS records

Replace `yourdomain.example` and `203.0.113.10` throughout. **Every record here
is DNS only — grey cloud.** The two web records may be proxied if you want
Cloudflare in front of them; the mail records may not.

| Type | Name | Value | Proxy | Notes |
|---|---|---|---|---|
| `A` | `mx1` | `203.0.113.10` | **DNS only** | The mail host. Must not be proxied. |
| `MX` | `@` | `mx1.yourdomain.example` (priority 10) | n/a | Points at the A record above, never at an IP. |
| `A` | `app` | `203.0.113.10` | proxied ok | The inbox app. |
| `TXT` | `@` | `v=spf1 -all` | n/a | See below. |
| `TXT` | `_dmarc` | `v=DMARC1; p=reject; rua=mailto:dmarc@yourdomain.example` | n/a | See below. |

Set the PTR record for `203.0.113.10` to `mx1.yourdomain.example` in your
host's control panel, not in Cloudflare — reverse DNS is delegated by whoever
owns the IP block.

### Why SPF and DMARC on a domain that sends nothing

Publishing `v=spf1 -all` says: nothing is authorised to send mail as this
domain. That is exactly true, and it stops the domain being useful for
spoofing. `p=reject` says the same thing in DMARC's language. A receive-only
domain that publishes neither is a free identity for anyone who wants one.

Do not publish DKIM records. There is no signing key because there is nothing
to sign.

### Public-pool domains

Each public-pool domain needs its own `MX` and its own `A` record for the mail
host, and the same SPF and DMARC pair. They can point at the same IP. The
separation that matters is reputational, not physical: when a public domain gets
blocklisted, the random-pool domains carry on, which is the entire reason the
pools exist.

## The marketing site

`site/` is excluded from the Docker image and from `make build`, so it never
ships to a self-hoster. Deploy it separately.

**Vercel** is the path of least resistance for Next.js:

- Root directory: `site`
- Build command: `npm run build --workspace site` from the repository root
- Environment: `NEXT_PUBLIC_SITE_URL`, `NEXT_PUBLIC_APP_URL`

**Cloudflare Pages** works too and keeps everything on one account. Either way,
point the apex `A`/`CNAME` at the deployment and leave `app` pointing at the
binary.

The site reads `api/openapi.yaml` at build time to generate its API reference,
so it must be built from the repository root rather than from `site/` alone.

## Secrets

`PHENK_MASTER_KEY` is the one that matters. Generate it once:

```sh
./bin/phenk genkey
```

Losing it destroys every inbox, which is the point of it: expiry works by
destroying keys, and a master key that can be recovered from a backup is a
master key that never really destroyed anything. Store it where you store
things you cannot regenerate, and do not put it in the repository, the image, or
a CI variable that logs.

## Before the first real message

1. `phenk migrate`
2. `phenk domain add yourdomain.example random active`
3. `phenk domain add public.yourdomain.example public active`
4. Send yourself a message and read it in the app.

`phenk domain list` shows what is currently allocatable. A domain added without
`active` starts as `fresh`, which means it accepts mail for identities it
already hosts but hands out no new addresses — which is what you want while a
new domain is warming up.
