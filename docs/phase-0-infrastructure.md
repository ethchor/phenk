# Phase 0 — infrastructure proof

This phase cannot be completed from a build container. It needs a registered
domain, DNS control, a host with inbound port 25, and a real mailbox at Gmail
and at Outlook. Everything below is a checklist for a human with those things.

**Nothing in Phase 1 depends on this being done. Phase 2 does.** If inbound
port 25 turns out to be blocked, the SMTP listener has nowhere to run and the
hosting decision has to be revisited before ingestion is built.

## Checklist

1. **Register a domain** and point its MX record at the host:

   ```
   phenk.example.  300  IN  MX  10 mx1.phenk.example.
   mx1.phenk.example. 300 IN A   <host ip>
   ```

   Add an SPF record and a PTR (reverse DNS) entry for the host. Neither
   affects inbound mail, but both affect how the domain's reputation is
   judged later.

2. **Confirm inbound port 25 is open.** Many providers block it silently and
   without notice — DigitalOcean, Google Cloud, Azure, Vultr and Oracle all
   block outbound by default, and several block or filter inbound too. From
   another machine:

   ```
   nc -vz mx1.phenk.example 25
   ```

   A hang rather than a refusal usually means a provider filter, not a
   firewall rule you control.

3. **Obtain a TLS certificate** usable by both the SMTP listener and the web
   tier. A single certificate covering `mx1.phenk.example` and
   `app.phenk.example` is fine for v0.

4. **Run the sink** on the host:

   ```
   go run ./tools/smtpsink -addr :25 -dir ./inbox -hostname mx1.phenk.example
   ```

   It accepts every recipient and writes each message to a `.eml` file. That
   is deliberately the opposite of what the real server does — Phenk rejects
   unknown recipients at `RCPT TO` rather than accepting and dropping — and it
   is correct here, because the question being asked is only whether packets
   arrive.

5. **Send real mail** to any address at the domain, once from Gmail and once
   from Outlook.

## Acceptance

Two `.eml` files on disk, from two different real providers.

Keep them. They become the first golden fixtures in `testdata/mime/` for the
Phase 3 parser, and messages from real providers exercise header and encoding
edge cases that hand-written fixtures do not.

## If port 25 is blocked

Stop and report before proceeding. The options, roughly in order of how much
of the plan they preserve:

- move the MX host to a provider that permits inbound 25 (Hetzner, OVH, and
  most bare-metal providers do);
- keep the application where it is and run only the SMTP listener elsewhere,
  pointing it at the same database;
- accept mail through a third-party inbound relay, which changes the trust
  model and means the `client_ip` and TLS fields on a delivery describe the
  relay rather than the sender.

The third option weakens SPF evaluation in Phase 3, so it is a last resort
rather than a shortcut.
