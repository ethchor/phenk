---
title: "Rendering email from strangers"
description: "Email HTML is the most hostile input a mail client handles. Two independent defences, because one is a single point of failure."
date: "2026-02-03"
---

Every message Phenk shows you was written by someone you have never met, for a
renderer they hoped would be lenient. Email HTML is the most hostile input the
system handles, and it arrives constantly.

There are two ways to get this wrong. The first is to sanitize and then trust
the result. The second is to sandbox and then not bother sanitizing. Both are one
mistake away from an incident, so we do both.

## What sanitizing removes

Scripts, obviously. Also forms, frames, embedded objects, `javascript:` links,
event handlers, and every CSS property that could move content out of its box or
fetch something remote. Anything not on the allowlist is gone.

Inline styles survive, against a list of properties that cannot escape or fetch.
Stripping styling entirely would be safer and would also leave every message
looking broken, which is not a trade anybody wants.

## What the sandbox does

The sanitized body is then rendered inside an iframe with `allow-popups` and
nothing else. In particular it has neither `allow-scripts` nor
`allow-same-origin`, which together give the frame an opaque origin and no
scripting at all.

That is the important part. Sanitizing is a filter, and filters have bugs. The
sandbox is what makes a bug in the filter survivable rather than fatal.

`dangerouslySetInnerHTML` is used for message content nowhere in the codebase,
sanitized or not.

## Images

Remote images are rewritten to load through the server. Left alone, a tracking
pixel reports your IP address, your user agent, and the exact moment you opened
the message, straight back to whoever sent it — which is precisely the tracking a
throwaway address exists to avoid.

The proxy will only fetch URLs it signed itself, refuses every private address
range including after a redirect, and serves nothing that is not an image.

## Authentication results

Every message shows SPF, DKIM and DMARC. `none` is rendered in a neutral colour,
never a green one: it means the check was not evaluated or the sender published
nothing to check against, and colouring it like a pass would be a lie a reader
would act on.
