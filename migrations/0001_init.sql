-- Phenk v0 initial schema.
--
-- Two rules are enforced here rather than left to application code, because
-- they are hard invariants and a CHECK constraint cannot be forgotten during a
-- refactor:
--
--   * a named identity has no owner and no expiry (invariant 9: a shared inbox
--     can never hold an owner session, a grant, or a webhook target);
--   * an address is unique over the whole table with no partial predicate, so
--     a purged or reserved address is never reallocated (invariant 5).

-- Domains are a reputation-bearing, rotating resource. The pools never mix:
-- public domains attract far more spam and get blocklisted faster, and keeping
-- them separate contains the damage to the pool that earned it.
CREATE TABLE domains (
  id         uuid PRIMARY KEY,
  name       text UNIQUE NOT NULL,
  state      text NOT NULL CHECK (state IN ('fresh', 'active', 'burned', 'retired')),
  pool       text NOT NULL CHECK (pool IN ('random', 'public')),
  created_at timestamptz NOT NULL DEFAULT now(),
  burned_at  timestamptz
);

CREATE INDEX domains_allocatable ON domains (pool) WHERE state = 'active';

CREATE TABLE identities (
  id               uuid PRIMARY KEY,
  local_part       text NOT NULL,
  domain_id        uuid NOT NULL REFERENCES domains (id),
  kind             text NOT NULL CHECK (kind IN ('random', 'named')),
  state            text NOT NULL CHECK (state IN ('active', 'expiring', 'expired', 'purged', 'reserved')),
  owner_session    text,
  wrapped_data_key bytea NOT NULL,
  retention_hours  int,
  delivery_seq     bigint NOT NULL DEFAULT 0,
  quota_messages   int NOT NULL DEFAULT 200,
  quota_bytes      bigint NOT NULL DEFAULT 104857600,
  used_messages    int NOT NULL DEFAULT 0,
  used_bytes       bigint NOT NULL DEFAULT 0,
  created_at       timestamptz NOT NULL DEFAULT now(),
  expires_at       timestamptz,
  purged_at        timestamptz,
  reserved_until   timestamptz,

  -- Invariant 9, structurally. A named inbox is shared by construction, so it
  -- can never carry the owner session that scoped access is issued against.
  CONSTRAINT named_identities_have_no_owner_or_expiry CHECK (
    kind <> 'named' OR (owner_session IS NULL AND expires_at IS NULL)
  ),
  CONSTRAINT usage_is_not_negative CHECK (used_messages >= 0 AND used_bytes >= 0)
);

-- Invariant 5: the address is never reallocated, so uniqueness covers every
-- row including purged and reserved tombstones. No partial predicate here,
-- deliberately.
CREATE UNIQUE INDEX identities_address ON identities (local_part, domain_id);
CREATE INDEX identities_expiry ON identities (expires_at) WHERE state = 'active';
CREATE INDEX identities_owner ON identities (owner_session) WHERE owner_session IS NOT NULL;
CREATE INDEX identities_purgeable ON identities (state) WHERE state = 'expired';
CREATE INDEX identities_named_sweep ON identities (kind) WHERE kind = 'named';

-- Local parts that may never be provisioned as a named inbox. Seeded with the
-- addresses an operator is obliged to control, plus room for a brand denylist
-- that grows without a deploy.
CREATE TABLE blocked_local_parts (
  local_part text PRIMARY KEY,
  reason     text NOT NULL
);

INSERT INTO blocked_local_parts (local_part, reason) VALUES
  ('postmaster', 'rfc5321 required'),
  ('abuse',      'rfc2142 required'),
  ('admin',      'operational'),
  ('root',       'operational'),
  ('security',   'rfc2142 required'),
  ('noreply',    'impersonation risk'),
  ('no-reply',   'impersonation risk'),
  ('support',    'impersonation risk'),
  ('billing',    'impersonation risk'),
  ('hostmaster', 'rfc2142 required'),
  ('webmaster',  'rfc2142 required'),
  ('dmarc',      'operational'),
  ('mailer-daemon', 'operational');

-- Raw MIME and extracted attachments. Content-addressed and immutable; shared
-- by refcount so one message delivered to two identities is stored once.
CREATE TABLE blobs (
  id         uuid PRIMARY KEY,
  sha256     bytea UNIQUE NOT NULL,
  size_bytes bigint NOT NULL,
  path       text NOT NULL,
  refcount   int NOT NULL DEFAULT 0 CHECK (refcount >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX blobs_orphaned ON blobs (id) WHERE refcount = 0;

CREATE TABLE deliveries (
  id            uuid PRIMARY KEY,
  identity_id   uuid NOT NULL REFERENCES identities (id),
  seq           bigint NOT NULL CHECK (seq > 0),
  blob_id       uuid NOT NULL REFERENCES blobs (id),
  envelope_from text NOT NULL,
  client_ip     inet NOT NULL,
  helo          text,
  tls           boolean NOT NULL,
  size_bytes    bigint NOT NULL,
  spf           text,
  dkim          text,
  dmarc         text,
  state         text NOT NULL CHECK (state IN ('received', 'parsed', 'failed')),
  received_at   timestamptz NOT NULL DEFAULT now(),
  parsed_at     timestamptz
);

-- The cursor clients page and wait on: monotonic and gapless per identity.
CREATE UNIQUE INDEX deliveries_cursor ON deliveries (identity_id, seq);
CREATE INDEX deliveries_blob ON deliveries (blob_id);
CREATE INDEX deliveries_retention ON deliveries (identity_id, received_at);

-- Derived output. Always rebuildable from the blob, so it can be dropped and
-- regenerated at will. Bodies are encrypted under the identity data key.
CREATE TABLE parsed_messages (
  delivery_id uuid PRIMARY KEY REFERENCES deliveries (id) ON DELETE CASCADE,
  subject     text,
  from_name   text,
  from_addr   text,
  to_addrs    text[],
  sent_at     timestamptz,
  text_body   bytea,
  html_body   bytea,
  preview     text,
  tsv         tsvector
);

CREATE INDEX parsed_messages_tsv ON parsed_messages USING gin (tsv);

CREATE TABLE attachments (
  id           uuid PRIMARY KEY,
  delivery_id  uuid NOT NULL REFERENCES deliveries (id) ON DELETE CASCADE,
  filename     text,
  content_type text,
  size_bytes   bigint,
  blob_id      uuid REFERENCES blobs (id)
);

CREATE INDEX attachments_delivery ON attachments (delivery_id);
CREATE INDEX attachments_blob ON attachments (blob_id);

CREATE TABLE events (
  seq         bigserial PRIMARY KEY,
  identity_id uuid,
  type        text NOT NULL,
  payload     jsonb NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX events_identity ON events (identity_id, seq);
