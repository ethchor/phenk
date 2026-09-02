package pg

import (
	"context"

	"github.com/ethchor/phenk/internal/core"
)

const deliveryColumns = `id, identity_id, seq, blob_id, envelope_from, client_ip, helo,
	tls, size_bytes, spf, dkim, dmarc, state, received_at, parsed_at`

// ReserveDeliverySlot takes the identity's row lock, allocates the next
// delivery sequence number, and charges the message against the identity's
// quota, all in one statement.
//
// Sequence numbers must be gapless because clients page and wait on them. That
// holds because the row lock serializes concurrent deliveries and the increment
// rolls back with the transaction, so a delivery that is never committed never
// consumes a number.
func ReserveDeliverySlot(ctx context.Context, q Querier, identityID core.UUID, sizeBytes int64) (int64, error) {
	var seq int64
	err := q.QueryRow(ctx, `
		UPDATE identities
		   SET delivery_seq  = delivery_seq + 1,
		       used_messages = used_messages + 1,
		       used_bytes    = used_bytes + $2
		 WHERE id = $1
		 RETURNING delivery_seq`, identityID, sizeBytes).Scan(&seq)
	if err != nil {
		return 0, mapError(err)
	}
	return seq, nil
}

// InsertDelivery writes a delivery row with an already-reserved sequence
// number.
func InsertDelivery(ctx context.Context, q Querier, d *core.Delivery) error {
	if d.ID.IsZero() {
		d.ID = core.NewUUID()
	}
	err := q.QueryRow(ctx, `
		INSERT INTO deliveries (
			id, identity_id, seq, blob_id, envelope_from, client_ip, helo, tls,
			size_bytes, spf, dkim, dmarc, state, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, COALESCE($14, now()))
		RETURNING received_at`,
		d.ID, d.IdentityID, d.Seq, d.BlobID, d.EnvelopeFrom, d.ClientIP, nullString(d.HELO),
		d.TLS, d.SizeBytes, nullString(string(d.SPF)), nullString(string(d.DKIM)),
		nullString(string(d.DMARC)), d.State, nullTime(d.ReceivedAt),
	).Scan(&d.ReceivedAt)
	return mapError(err)
}

// DeliveryByID reads one delivery.
func DeliveryByID(ctx context.Context, q Querier, id core.UUID) (*core.Delivery, error) {
	return scanDelivery(q.QueryRow(ctx,
		`SELECT `+deliveryColumns+` FROM deliveries WHERE id = $1`, id))
}

// DeliveriesSince lists an identity's deliveries after a cursor, in sequence
// order. This is the query behind both the message list and the first half of
// wait: wait asks it before it subscribes to anything, so a message that
// arrived in the gap between creating an identity and waiting on it is
// returned rather than missed. That is invariant 6.
func DeliveriesSince(ctx context.Context, q Querier, identityID core.UUID, since int64, limit int) ([]core.Delivery, error) {
	rows, err := q.Query(ctx,
		`SELECT `+deliveryColumns+`
		   FROM deliveries WHERE identity_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`,
		identityID, since, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, mapError(rows.Err())
}

// MarkDeliveryParsed records a successful parse together with the mail
// authentication results, which are only known once the raw message has been
// read.
func MarkDeliveryParsed(ctx context.Context, q Querier, id core.UUID, spf, dkim, dmarc core.AuthResult) error {
	tag, err := q.Exec(ctx, `
		UPDATE deliveries
		   SET state = 'parsed', parsed_at = now(), spf = $2, dkim = $3, dmarc = $4
		 WHERE id = $1`,
		id, nullString(string(spf)), nullString(string(dkim)), nullString(string(dmarc)))
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkDeliveryFailed records that a message could not be parsed. The blob is
// kept and the raw message stays readable regardless.
func MarkDeliveryFailed(ctx context.Context, q Querier, id core.UUID) error {
	tag, err := q.Exec(ctx,
		`UPDATE deliveries SET state = 'failed', parsed_at = now() WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanDelivery(row rowScanner) (*core.Delivery, error) {
	var (
		d                core.Delivery
		helo             *string
		spf, dkim, dmarc *string
	)
	err := row.Scan(&d.ID, &d.IdentityID, &d.Seq, &d.BlobID, &d.EnvelopeFrom, &d.ClientIP,
		&helo, &d.TLS, &d.SizeBytes, &spf, &dkim, &dmarc, &d.State, &d.ReceivedAt, &d.ParsedAt)
	if err != nil {
		return nil, mapError(err)
	}
	d.HELO = textOrEmpty(helo)
	d.SPF = core.AuthResult(textOrEmpty(spf))
	d.DKIM = core.AuthResult(textOrEmpty(dkim))
	d.DMARC = core.AuthResult(textOrEmpty(dmarc))
	return &d, nil
}
