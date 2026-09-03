# Production deployment

A single host running Phenk, PostgreSQL, and Caddy. See
[../docs/deployment.md](../docs/deployment.md) for choosing that host and for
the DNS records, which matter more than anything in this directory.

## Before you start

Confirm inbound port 25 actually reaches the host. Nothing else here is worth
doing until that is true:

```sh
go run ./tools/smtpsink -addr :25 -dir ./inbox -hostname mx1.yourdomain.example
```

Send it mail from Gmail and from Outlook. Two `.eml` files on disk is the only
evidence that counts.

## Deploy

```sh
cp deploy/.env.example deploy/.env
# Fill in .env. Generate the master key with:  go run ./cmd/phenk genkey

docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env up -d --build
```

Migrations run at startup. Then add the domains:

```sh
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env \
  exec phenk phenk domain add yourdomain.example random active
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env \
  exec phenk phenk domain add public.yourdomain.example public active
```

A domain added without `active` starts as `fresh`: it accepts mail for
identities it already hosts but hands out no new addresses. That is what you
want while a new domain warms up.

## The certificate

Caddy obtains certificates for both hostnames. It keeps them in its own storage,
under a directory named after whichever ACME issuer succeeded, and has no
directive that writes one to a path of your choosing — so the `certsync`
service copies the mail hostname's certificate into a volume Phenk reads for
STARTTLS. One certificate, one renewal, serving both the web tier and the mail
listener.

It copies only when the content differs, which is what makes renewal work: the
listener decides whether to re-read the certificate by looking at its
modification time, and picks up a renewal on its own without a restart.

**On a first deployment, restart Phenk once the certificate has been issued:**

```sh
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env logs certsync
# wait for "installed certificate for ..."
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env restart phenk
```

Phenk starts before any certificate exists and logs `smtp tls certificate not
present, starting without starttls`. That is deliberate. STARTTLS is
opportunistic, so a listener without a certificate still accepts mail in the
clear; a listener that advertised STARTTLS and then failed the handshake would
lose the senders that took it up, because most give up rather than retrying
unencrypted. Only the first boot needs this — renewals are picked up live.

## Why the container listens on 2525

The image runs as an unprivileged user, and a process that is not root cannot
bind a port below 1024. The container listens on 2525 and the published mapping
`25:2525` puts it on 25 as far as any sender is concerned. Granting the
container `CAP_NET_BIND_SERVICE` instead would work and would buy nothing.

## Why port 25 is not behind the proxy

Caddy fronts 80 and 443. Port 25 is published straight to the Phenk container,
and it has to be. A proxy in front of the SMTP listener replaces the sender's
address with its own, and the sender's address is exactly what SPF is evaluated
against — proxying it would silently make every SPF result meaningless.

## Backups

Two things, and they are not equally recoverable:

- **PostgreSQL** — ordinary `pg_dump`. Holds identities, deliveries, and the
  wrapped keys.
- **`PHENK_MASTER_KEY`** — store it wherever you keep things that cannot be
  regenerated. Every wrapped key in the database is useless without it, so a
  database backup alone restores nothing.

The blob volume is worth backing up if you want message contents to survive a
host loss, but it is the least critical of the three: without the master key the
blobs are unreadable anyway.

## Upgrading

```sh
git pull
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env up -d --build
```

Migrations are guarded by an advisory lock and are safe to run from several
processes at once, so a rolling restart will not race.
