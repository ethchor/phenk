package pg

import (
	"context"
	"fmt"

	"github.com/ethchor/phenk/internal/core"
)

const domainColumns = `id, name, state, pool, created_at, burned_at`

// CreateDomain inserts a domain.
func CreateDomain(ctx context.Context, q Querier, d *core.Domain) error {
	if d.ID.IsZero() {
		d.ID = core.NewUUID()
	}
	if !d.State.Valid() || !d.Pool.Valid() {
		return fmt.Errorf("pg: domain %q has an invalid state or pool", d.Name)
	}
	err := q.QueryRow(ctx, `
		INSERT INTO domains (id, name, state, pool, created_at, burned_at)
		VALUES ($1, $2, $3, $4, COALESCE($5, now()), $6)
		RETURNING created_at`,
		d.ID, d.Name, d.State, d.Pool, nullTime(d.CreatedAt), d.BurnedAt,
	).Scan(&d.CreatedAt)
	return mapError(err)
}

// DomainByName resolves a domain from the RCPT TO address. It is on the hot
// SMTP path, behind a cache.
func DomainByName(ctx context.Context, q Querier, name string) (*core.Domain, error) {
	return scanDomain(q.QueryRow(ctx,
		`SELECT `+domainColumns+` FROM domains WHERE name = $1`, name))
}

// DomainByID resolves a domain by primary key.
func DomainByID(ctx context.Context, q Querier, id core.UUID) (*core.Domain, error) {
	return scanDomain(q.QueryRow(ctx,
		`SELECT `+domainColumns+` FROM domains WHERE id = $1`, id))
}

// AllocatableDomains lists the domains in a pool that may hand out new
// addresses. Burned domains still receive mail for identities they already
// host, so they are deliberately excluded here and nowhere else.
func AllocatableDomains(ctx context.Context, q Querier, pool core.Pool) ([]core.Domain, error) {
	rows, err := q.Query(ctx,
		`SELECT `+domainColumns+` FROM domains WHERE pool = $1 AND state = 'active' ORDER BY name`, pool)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Domain
	for rows.Next() {
		var d core.Domain
		if err := rows.Scan(&d.ID, &d.Name, &d.State, &d.Pool, &d.CreatedAt, &d.BurnedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, d)
	}
	return out, mapError(rows.Err())
}

// SetDomainState moves a domain through its reputation lifecycle, stamping
// burned_at when it is burned.
func SetDomainState(ctx context.Context, q Querier, id core.UUID, state core.DomainState) error {
	if !state.Valid() {
		return fmt.Errorf("pg: invalid domain state %q", state)
	}
	tag, err := q.Exec(ctx, `
		UPDATE domains
		   SET state = $2,
		       burned_at = CASE WHEN $2 = 'burned' THEN COALESCE(burned_at, now()) ELSE burned_at END
		 WHERE id = $1`, id, state)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanDomain(row rowScanner) (*core.Domain, error) {
	var d core.Domain
	err := row.Scan(&d.ID, &d.Name, &d.State, &d.Pool, &d.CreatedAt, &d.BurnedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &d, nil
}
