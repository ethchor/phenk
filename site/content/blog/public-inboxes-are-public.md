---
title: "Public inboxes are public, and we say so"
description: "The shared-inbox feature is genuinely useful and genuinely dangerous. Which half you get depends entirely on whether the interface tells you."
date: "2026-02-20"
---

You can open an inbox in Phenk by typing a name. Type `invoices` and you get
`invoices@` on one of the public domains, immediately, with whatever is already
in it.

So can everybody else.

## Why it exists

It is genuinely useful. There is no signup, no session to keep, and no address to
copy — you can be told a name over the phone and read the inbox from a different
device a minute later. For a throwaway signup on a shared machine that is exactly
right.

## Why it is dangerous

There is no owner, so there is nothing to check. Anyone who guesses the name
reads the mail. Common words are guessed immediately and constantly.

That is not a flaw to be fixed. It is what the feature is. A shared inbox that
checked who you were would be a different feature.

## The part that decides which one you got

Every screen that shows a public inbox says, in plain words and without being
dismissable, that anyone who guesses the name can read it. It is there before you
type a name, not after you have used one, because the moment you need to know is
the moment you are choosing.

A warning you can dismiss is not a warning. The fact does not stop being true
when the toast disappears.

## What follows from it

Public inboxes can never hold scoped access — no API keys, no webhooks, no
grants. Not as policy, but because there is nobody to grant anything to, and code
that pretended otherwise would be issuing credentials to the public.

The system refuses it at every layer that could issue one.
