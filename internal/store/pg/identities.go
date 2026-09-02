package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethchor/phenk/internal/core"
)

const identityColumns = `id, local_part, domain_id, kind, state, owner_session,
	wrapped_data_key, retention_hours, delivery_seq, quota_messages, quota_bytes,
	used_messages, used_bytes, created_at, expires_at, purged_at, reserved_until`

// CreateIdentity inserts an identity. The caller has already minted the data
// key, because an identity without one cannot exist: invariant 4.
func CreateIdentity(ctx context.Context, q Querier, id *core.Identity) error {
	if id.ID.IsZero() {
		id.ID = core.NewUUID()
	}
	if err := validateIdentity(id); err != nil {
		return err
	}
	err := q.QueryRow(ctx, `
		INSERT INTO identities (
			id, local_part, domain_id, kind, state, owner_session, wrapped_data_key,
			retention_hours, quota_messages, quota_bytes, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, now()), $12)
		RETURNING created_at`,
		id.ID, id.LocalPart, id.DomainID, id.Kind, id.State, nullString(id.OwnerSession),
		id.WrappedDataKey, id.RetentionHours, id.QuotaMessages, id.QuotaBytes,
		nullTime(id.CreatedAt), id.ExpiresAt,
	).Scan(&id.CreatedAt)
	return mapError(err)
}

// CreateIdentityIfAbsent is the lazy provisioning path for named inboxes. Two
// senders can hit a never-seen name at the same moment, so the insert is a
// no-op on conflict and the row is read back unconditionally: both sessions end
// up with the same identity, and exactly one of them created it.
//
// It returns the identity and whether this call was the one that created it,
// so the caller knows whether to emit identity.created.
func CreateIdentityIfAbsent(ctx context.Context, q Querier, id *core.Identity) (*core.Identity, bool, error) {
	if id.ID.IsZero() {
		id.ID = core.NewUUID()
	}
	if err := validateIdentity(id); err != nil {
		return nil, false, err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO identities (
			id, local_part, domain_id, kind, state, owner_session, wrapped_data_key,
			retention_hours, quota_messages, quota_bytes, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, now()), $12)
		ON CONFLICT (local_part, domain_id) DO NOTHING`,
		id.ID, id.LocalPart, id.DomainID, id.Kind, id.State, nullString(id.OwnerSession),
		id.WrappedDataKey, id.RetentionHours, id.QuotaMessages, id.QuotaBytes,
		nullTime(id.CreatedAt), id.ExpiresAt)
	if err != nil {
		return nil, false, mapError(err)
	}

	stored, err := IdentityByAddress(ctx, q, id.LocalPart, id.DomainID)
	if err != nil {
		return nil, false, err
	}
	return stored, stored.ID == id.ID, nil
}

// IdentityByID looks an identity up by primary key.
func IdentityByID(ctx context.Context, q Querier, id core.UUID) (*core.Identity, error) {
	return scanIdentity(q.QueryRow(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE id = $1`, id))
}

// IdentityByAddress looks an identity up by its address. The local part must
// already be normalized.
func IdentityByAddress(ctx context.Context, q Querier, localPart string, domainID core.UUID) (*core.Identity, error) {
	return scanIdentity(q.QueryRow(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE local_part = $1 AND domain_id = $2`,
		localPart, domainID))
}

// IdentityForUpdate re-reads an identity and holds a row lock for the rest of
// the transaction. The SMTP commit path uses it to serialize delivery_seq
// allocation, which is what keeps the client cursor gapless.
func IdentityForUpdate(ctx context.Context, q Querier, id core.UUID) (*core.Identity, error) {
	return scanIdentity(q.QueryRow(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE id = $1 FOR UPDATE`, id))
}

// Recipient is an identity resolved together with the domain it lives on,
// which is what the RCPT TO handler needs: the pool decides whether an unknown
// local part is a rejection or a lazy provision.
type Recipient struct {
	Domain   core.Domain
	Identity *core.Identity // nil when the address has never existed
}

// ResolveRecipient looks up the domain and then, if it exists, the identity at
// that address. A missing domain is ErrNotFound — we are not the MX for it at
// all — while a missing identity is a nil Identity, because those two cases
// lead to different SMTP replies.
//
// The two lookups are separate on purpose: the RCPT TO handler caches domain
// resolution, so in steady state only the identity lookup reaches the database.
func ResolveRecipient(ctx context.Context, q Querier, localPart, domainName string) (*Recipient, error) {
	domain, err := DomainByName(ctx, q, domainName)
	if err != nil {
		return nil, err
	}
	r := &Recipient{Domain: *domain}

	identity, err := IdentityByAddress(ctx, q, localPart, domain.ID)
	switch {
	case err == nil:
		r.Identity = identity
	case errors.Is(err, ErrNotFound):
		// Left nil: the address has never existed.
	default:
		return nil, err
	}
	return r, nil
}

// IdentitiesByOwner lists the identities a session owns, newest first. Named
// identities can never appear here: they have no owner.
func IdentitiesByOwner(ctx context.Context, q Querier, session string) ([]core.Identity, error) {
	if session == "" {
		return nil, nil
	}
	rows, err := q.Query(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE owner_session = $1 ORDER BY created_at DESC`,
		session)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Identity
	for rows.Next() {
		id, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *id)
	}
	return out, mapError(rows.Err())
}

// SetIdentityState moves an identity to a new state, but only from one of the
// states given in from. Guarding the transition in the WHERE clause is what
// makes the lifecycle jobs idempotent: a second run matches no rows and
// changes nothing. It reports whether a row actually moved.
func SetIdentityState(ctx context.Context, q Querier, id core.UUID, to core.IdentityState, from ...core.IdentityState) (bool, error) {
	if !to.Valid() {
		return false, fmt.Errorf("pg: invalid identity state %q", to)
	}
	states := make([]string, len(from))
	for i, s := range from {
		states[i] = string(s)
	}
	tag, err := q.Exec(ctx, `
		UPDATE identities SET state = $2
		 WHERE id = $1 AND (cardinality($3::text[]) = 0 OR state = ANY($3::text[]))`,
		id, to, states)
	if err != nil {
		return false, mapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// DestroyDataKey performs the durable half of a purge: it overwrites the
// wrapped data key, which is the only thing that can decrypt anything the
// identity received, and moves the identity to the reserved tombstone state.
// The row itself is never deleted, so the address is never reallocated.
//
// It is guarded on the current state, so running purge twice is a no-op the
// second time.
func DestroyDataKey(ctx context.Context, q Querier, id core.UUID, reservedUntil time.Time) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE identities
		   SET wrapped_data_key = ''::bytea,
		       state = 'reserved',
		       purged_at = COALESCE(purged_at, now()),
		       reserved_until = $2
		 WHERE id = $1 AND state IN ('expired', 'purged')`, id, reservedUntil)
	if err != nil {
		return false, mapError(err)
	}
	return tag.RowsAffected() > 0, nil
}

// IsLocalPartBlocked reports whether a name may never be provisioned. The list
// is data rather than code so it can grow without a deploy, which matters for
// the brand denylist.
func IsLocalPartBlocked(ctx context.Context, q Querier, localPart string) (bool, error) {
	var blocked bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM blocked_local_parts WHERE local_part = $1)`, localPart).
		Scan(&blocked)
	return blocked, mapError(err)
}

// BlockLocalPart adds a name to the denylist.
func BlockLocalPart(ctx context.Context, q Querier, localPart, reason string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO blocked_local_parts (local_part, reason) VALUES ($1, $2)
		ON CONFLICT (local_part) DO UPDATE SET reason = EXCLUDED.reason`, localPart, reason)
	return mapError(err)
}

func validateIdentity(id *core.Identity) error {
	if !id.Kind.Valid() {
		return fmt.Errorf("pg: invalid identity kind %q", id.Kind)
	}
	if !id.State.Valid() {
		return fmt.Errorf("pg: invalid identity state %q", id.State)
	}
	if len(id.WrappedDataKey) == 0 {
		return fmt.Errorf("pg: identity %s has no data key", id.LocalPart)
	}
	// Invariant 9, checked before the database checks it, so the error names
	// the actual rule rather than a constraint.
	if id.Kind == core.KindNamed {
		if id.OwnerSession != "" {
			return fmt.Errorf("%w: %s", core.ErrNotEligibleForAuth, id.LocalPart)
		}
		if id.ExpiresAt != nil {
			return fmt.Errorf("pg: named identity %s cannot expire", id.LocalPart)
		}
	}
	return nil
}

func scanIdentity(row rowScanner) (*core.Identity, error) {
	var (
		i     core.Identity
		owner *string
	)
	err := row.Scan(&i.ID, &i.LocalPart, &i.DomainID, &i.Kind, &i.State, &owner,
		&i.WrappedDataKey, &i.RetentionHours, &i.DeliverySeq, &i.QuotaMessages,
		&i.QuotaBytes, &i.UsedMessages, &i.UsedBytes, &i.CreatedAt, &i.ExpiresAt,
		&i.PurgedAt, &i.ReservedUntil)
	if err != nil {
		return nil, mapError(err)
	}
	i.OwnerSession = textOrEmpty(owner)
	return &i, nil
}

// NamedIdentityByLocalPart finds an existing named inbox by name alone, across
// every public-pool domain.
//
// A name has to resolve to one inbox: someone who types "invoices" into the
// switcher twice must reach the same messages both times, and must not quietly
// acquire a second inbox on a second domain. If the pool composition changes
// under an existing name, this lookup is what keeps that name pinned to the
// inbox it already has.
func NamedIdentityByLocalPart(ctx context.Context, q Querier, localPart string) (*core.Identity, *core.Domain, error) {
	var (
		i     core.Identity
		d     core.Domain
		owner *string
	)
	err := q.QueryRow(ctx, `
		SELECT i.id, i.local_part, i.domain_id, i.kind, i.state, i.owner_session,
		       i.wrapped_data_key, i.retention_hours, i.delivery_seq, i.quota_messages,
		       i.quota_bytes, i.used_messages, i.used_bytes, i.created_at, i.expires_at,
		       i.purged_at, i.reserved_until,
		       d.id, d.name, d.state, d.pool, d.created_at, d.burned_at
		  FROM identities i
		  JOIN domains d ON d.id = i.domain_id
		 WHERE i.local_part = $1 AND i.kind = 'named'
		 ORDER BY i.created_at
		 LIMIT 1`, localPart).
		Scan(&i.ID, &i.LocalPart, &i.DomainID, &i.Kind, &i.State, &owner,
			&i.WrappedDataKey, &i.RetentionHours, &i.DeliverySeq, &i.QuotaMessages,
			&i.QuotaBytes, &i.UsedMessages, &i.UsedBytes, &i.CreatedAt, &i.ExpiresAt,
			&i.PurgedAt, &i.ReservedUntil,
			&d.ID, &d.Name, &d.State, &d.Pool, &d.CreatedAt, &d.BurnedAt)
	if err != nil {
		return nil, nil, mapError(err)
	}
	i.OwnerSession = textOrEmpty(owner)
	return &i, &d, nil
}
