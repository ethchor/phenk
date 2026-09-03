---
title: "What a disposable address is actually for"
description: "Not hiding from anyone. Just declining to hand a permanent identifier to a shop that wanted an email field."
date: "2026-01-15"
---

Most explanations of disposable email start from the wrong place. They talk
about anonymity, as though the point were to be untraceable. It usually is not.

The ordinary case is much smaller than that. A site wants an email address
before it will let you read a PDF, or download a trial, or see a price. You are
not hiding. You are declining to hand over a permanent identifier — one that
works forever, that you cannot revoke, and that will be sold or breached
eventually — in exchange for something you want once.

## The asymmetry

An email address is a strange thing to give away casually. It never expires. It
is the recovery route for most of your other accounts. And once it is in a
database, you have no way to take it back, no way to tell who leaked it, and no
way to stop what happens next.

A disposable address inverts that. It works for exactly as long as you need it
and then stops existing. If the site leaks its database next year, the address
in it goes nowhere.

## What "expires" has to mean

The word does a lot of work, so it is worth being precise about it. In Phenk,
expiry destroys the encryption key that the messages were stored under. The
messages are not hidden, or flagged deleted, or moved to a table nobody queries.
They stop being readable, including by us.

That distinction only matters on the day somebody asks for the data, and on that
day it matters completely.

## What it is not for

A disposable address is a bad idea for anything you will need to get back into.
Password resets, banking, anything with money attached — those need an address
that will still be yours next year. Using a throwaway for them is not clever
privacy, it is a locked door with the key thrown away.

The useful version of this tool is narrow and honest about being narrow.
