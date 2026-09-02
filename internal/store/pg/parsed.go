package pg

import (
	"context"

	"github.com/ethchor/phenk/internal/core"
)

// UpsertParsedMessage writes derived output for a delivery.
//
// It is an upsert because parsed output is always rebuildable from the raw
// blob: re-running the parser over a message must be safe, whether because a
// retry raced a success or because a parser fix is being applied to old mail.
//
// The search vector is built here rather than by a trigger so that the weights
// live next to the query that uses them: a subject match should outrank a body
// match.
func UpsertParsedMessage(ctx context.Context, q Querier, m *core.ParsedMessage, searchText string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO parsed_messages (
			delivery_id, subject, from_name, from_addr, to_addrs, sent_at,
			text_body, html_body, preview, tsv)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
			setweight(to_tsvector('simple', coalesce($2, '')), 'A') ||
			setweight(to_tsvector('simple', coalesce($10, '')), 'B'))
		ON CONFLICT (delivery_id) DO UPDATE SET
			subject   = EXCLUDED.subject,
			from_name = EXCLUDED.from_name,
			from_addr = EXCLUDED.from_addr,
			to_addrs  = EXCLUDED.to_addrs,
			sent_at   = EXCLUDED.sent_at,
			text_body = EXCLUDED.text_body,
			html_body = EXCLUDED.html_body,
			preview   = EXCLUDED.preview,
			tsv       = EXCLUDED.tsv`,
		m.DeliveryID, nullString(m.Subject), nullString(m.FromName), nullString(m.FromAddr),
		m.ToAddrs, m.SentAt, m.TextBody, m.HTMLBody, nullString(m.Preview), searchText)
	return mapError(err)
}

// ParsedMessageByDelivery reads derived output. The bodies come back still
// encrypted; only a holder of the identity's data key can read them.
func ParsedMessageByDelivery(ctx context.Context, q Querier, deliveryID core.UUID) (*core.ParsedMessage, error) {
	var (
		m                           core.ParsedMessage
		subject, fromName, fromAddr *string
		preview                     *string
	)
	err := q.QueryRow(ctx, `
		SELECT delivery_id, subject, from_name, from_addr, to_addrs, sent_at,
		       text_body, html_body, preview
		  FROM parsed_messages WHERE delivery_id = $1`, deliveryID).
		Scan(&m.DeliveryID, &subject, &fromName, &fromAddr, &m.ToAddrs, &m.SentAt,
			&m.TextBody, &m.HTMLBody, &preview)
	if err != nil {
		return nil, mapError(err)
	}
	m.Subject = textOrEmpty(subject)
	m.FromName = textOrEmpty(fromName)
	m.FromAddr = textOrEmpty(fromAddr)
	m.Preview = textOrEmpty(preview)
	return &m, nil
}

// ReplaceAttachments writes the attachment rows for a delivery, replacing any
// from an earlier parse. Like the parsed message itself, attachments are
// derived, so re-parsing must converge rather than accumulate duplicates.
func ReplaceAttachments(ctx context.Context, q Querier, deliveryID core.UUID, attachments []core.Attachment) error {
	if _, err := q.Exec(ctx, `DELETE FROM attachments WHERE delivery_id = $1`, deliveryID); err != nil {
		return mapError(err)
	}
	for i := range attachments {
		a := &attachments[i]
		if a.ID.IsZero() {
			a.ID = core.NewUUID()
		}
		a.DeliveryID = deliveryID
		_, err := q.Exec(ctx, `
			INSERT INTO attachments (id, delivery_id, filename, content_type, size_bytes, blob_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			a.ID, a.DeliveryID, nullString(a.Filename), nullString(a.ContentType), a.SizeBytes, a.BlobID)
		if err != nil {
			return mapError(err)
		}
	}
	return nil
}

// AttachmentsForDelivery lists a delivery's attachments.
func AttachmentsForDelivery(ctx context.Context, q Querier, deliveryID core.UUID) ([]core.Attachment, error) {
	rows, err := q.Query(ctx, `
		SELECT id, delivery_id, filename, content_type, size_bytes, blob_id
		  FROM attachments WHERE delivery_id = $1 ORDER BY id`, deliveryID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Attachment
	for rows.Next() {
		var (
			a                     core.Attachment
			filename, contentType *string
		)
		if err := rows.Scan(&a.ID, &a.DeliveryID, &filename, &contentType, &a.SizeBytes, &a.BlobID); err != nil {
			return nil, mapError(err)
		}
		a.Filename = textOrEmpty(filename)
		a.ContentType = textOrEmpty(contentType)
		out = append(out, a)
	}
	return out, mapError(rows.Err())
}

// SearchDeliveries finds an identity's messages matching a query, most recent
// first. Search is a Postgres tsvector and nothing more, deliberately.
func SearchDeliveries(ctx context.Context, q Querier, identityID core.UUID, query string, limit int) ([]core.Delivery, error) {
	rows, err := q.Query(ctx, `
		SELECT d.id, d.identity_id, d.seq, d.blob_id, d.envelope_from, d.client_ip, d.helo,
		       d.tls, d.size_bytes, d.spf, d.dkim, d.dmarc, d.state, d.received_at, d.parsed_at
		  FROM deliveries d
		  JOIN parsed_messages p ON p.delivery_id = d.id
		 WHERE d.identity_id = $1 AND p.tsv @@ websearch_to_tsquery('simple', $2)
		 ORDER BY ts_rank(p.tsv, websearch_to_tsquery('simple', $2)) DESC, d.seq DESC
		 LIMIT $3`, identityID, query, limit)
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
