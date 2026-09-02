-- Raw MIME blobs are encrypted under a per-blob content key, and each delivery
-- stores that key wrapped under its own identity's data key.
--
-- This reconciles two requirements that would otherwise contradict each other.
-- Blobs stay content-addressed and shared, so one message delivered to two
-- identities is stored once; and every identity's copy is still encrypted under
-- a key only it holds, so purging an identity destroys its access even though
-- the bytes remain for whoever else received the same message.
--
-- Encrypting the blob under an identity's key directly would have made the
-- ciphertext, and therefore the content address, different per identity, which
-- would have removed deduplication entirely.

ALTER TABLE deliveries ADD COLUMN wrapped_content_key bytea;

COMMENT ON COLUMN deliveries.wrapped_content_key IS
  'The blob content key, wrapped under this identity''s data key. Destroyed with the identity key on purge.';

-- The blob row records the size of what is stored, which is the ciphertext.
-- The delivery already records the size of the message itself, which is what a
-- quota charges and a user sees.
COMMENT ON COLUMN blobs.size_bytes IS 'Size of the stored bytes, including encryption overhead.';
COMMENT ON COLUMN deliveries.size_bytes IS 'Size of the message itself, before encryption.';
