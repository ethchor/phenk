# How raw messages are encrypted

The v0 plan states two things that cannot both be true as written:

- **Invariant 3** — raw MIME is immutable and content-addressed, and the Phase 2
  acceptance criteria require that one message delivered to two identities
  produces one blob and two deliveries.
- **Invariant 4** — every identity has a data encryption key, and blob contents
  are encrypted under it.

Encrypting a blob under an identity's key makes the ciphertext, and therefore
the content address, different for every recipient. Deduplication disappears
entirely, and the second recipient cannot read the first recipient's copy.

## What is implemented

Envelope encryption.

1. Each message gets a fresh random **content key** when it is committed.
2. The raw MIME is encrypted under that content key and written to the blob
   store. The blob is addressed by the hash of the stored bytes, so the bytes
   are shared exactly as before.
3. For every recipient, the content key is wrapped under **that identity's own
   data key** and stored on the delivery row as `wrapped_content_key`.

Reading a message means unwrapping the master key, then the identity data key,
then the content key, then the blob. Four steps, each of which can be revoked
independently of the others.

## Why this resolves it

Purging an identity destroys its data key, which destroys its wrapping of the
content key, which makes its copy of the message unreadable — even though the
bytes are still on disk for whoever else received the same message. Invariant 4's
actual guarantee, that purge destroys access, holds exactly. Invariant 3 holds
too: the blob is still content-addressed, immutable, and shared by refcount.

Encryption is streamed in 64KiB frames rather than done in one pass. A message
is capped at 25MB and the SMTP path spools it to disk specifically so that ten
simultaneous senders are not 250MB of heap; a one-shot encrypt would have given
all of that back. Each frame is bound to its position by the nonce counter and
the final frame is marked as final, so a truncated or reordered stream fails to
decrypt rather than yielding a message that quietly lost its ending.

## What this does not protect against

Someone holding the master key, the database, and the blob store can read any
message belonging to an identity that has not been purged. That is inherent:
the service has to be able to show the user their mail.

## Sizes

Two size columns mean two different things, and conflating them would misreport
quota:

- `deliveries.size_bytes` is the size of the message itself, before encryption.
  This is what a quota charges and what a user sees.
- `blobs.size_bytes` is the size of what was written to storage, including
  framing and authentication overhead.

## The alternative that was rejected

Deriving the content key deterministically from the message contents — convergent
encryption — would have preserved deduplication across separate SMTP
transactions as well, and needed no extra column. It was rejected because the
key would be re-derivable from the master key alone, so a purged identity's blob
would stay readable for as long as it survived on disk. Purge would protect
against a stolen blob store but not against the operator, which is a materially
weaker promise than the one invariant 4 makes.
