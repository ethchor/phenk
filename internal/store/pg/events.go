package pg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ethchor/phenk/internal/core"
)

// NotifyChannel is the LISTEN/NOTIFY channel every Phenk process subscribes to.
const NotifyChannel = "phenk_events"

// Notification is the payload sent over NotifyChannel. It carries an event's
// identity and sequence number rather than its contents: listeners read the row
// if they care, and the 8000-byte NOTIFY limit can never be a factor.
type Notification struct {
	Seq        int64      `json:"seq"`
	IdentityID *core.UUID `json:"identity_id,omitempty"`
	Type       string     `json:"type"`
}

// AppendEvent writes an event and notifies listeners in the same transaction,
// so a committed event is always announced and a rolled-back one never is.
func AppendEvent(ctx context.Context, q Querier, identityID *core.UUID, eventType string, payload any) (int64, error) {
	body := []byte("{}")
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("pg: encoding %s payload: %w", eventType, err)
		}
		body = encoded
	}

	var seq int64
	err := q.QueryRow(ctx, `
		INSERT INTO events (identity_id, type, payload) VALUES ($1, $2, $3) RETURNING seq`,
		identityID, eventType, body).Scan(&seq)
	if err != nil {
		return 0, mapError(err)
	}

	notice, err := json.Marshal(Notification{Seq: seq, IdentityID: identityID, Type: eventType})
	if err != nil {
		return 0, fmt.Errorf("pg: encoding notification: %w", err)
	}
	if _, err := q.Exec(ctx, `SELECT pg_notify($1, $2)`, NotifyChannel, string(notice)); err != nil {
		return 0, mapError(err)
	}
	return seq, nil
}

// EventsSince reads events after a cursor, oldest first. A subscriber that
// missed a notification while disconnected catches up here rather than losing
// the event.
func EventsSince(ctx context.Context, q Querier, since int64, limit int) ([]core.Event, error) {
	rows, err := q.Query(ctx,
		`SELECT seq, identity_id, type, payload, created_at
		   FROM events WHERE seq > $1 ORDER BY seq LIMIT $2`, since, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Event
	for rows.Next() {
		var e core.Event
		if err := rows.Scan(&e.Seq, &e.IdentityID, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, e)
	}
	return out, mapError(rows.Err())
}

// EventsForIdentity reads one identity's events after a cursor.
func EventsForIdentity(ctx context.Context, q Querier, identityID core.UUID, since int64, limit int) ([]core.Event, error) {
	rows, err := q.Query(ctx,
		`SELECT seq, identity_id, type, payload, created_at
		   FROM events WHERE identity_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`,
		identityID, since, limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var out []core.Event
	for rows.Next() {
		var e core.Event
		if err := rows.Scan(&e.Seq, &e.IdentityID, &e.Type, &e.Payload, &e.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, e)
	}
	return out, mapError(rows.Err())
}
