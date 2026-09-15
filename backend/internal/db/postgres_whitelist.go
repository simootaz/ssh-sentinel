package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// whitelistSelect joins the server name and the device label for display.
// LEFT JOIN: server_id is NULL for a global entry and created_by_device
// becomes NULL when the phone is deleted.
const whitelistSelect = `
	SELECT w.id::text, w.username, w.context, w.server_id::text, s.name,
		w.expires_at, w.created_at, w.created_from::text, w.created_by_device::text, d.label
	FROM whitelist w
	LEFT JOIN servers s ON s.id = w.server_id
	LEFT JOIN devices d ON d.id = w.created_by_device`

func scanWhitelist(row scanner) (*model.WhitelistEntry, error) {
	var e model.WhitelistEntry
	var serverID, serverName, createdFrom, device, label sql.NullString
	var expires sql.NullTime
	err := row.Scan(&e.ID, &e.Username, &e.Context, &serverID, &serverName,
		&expires, &e.CreatedAt, &createdFrom, &device, &label)
	if err != nil {
		return nil, err
	}
	e.ServerID = strPtr(serverID)
	e.ServerName = strPtr(serverName)
	e.ExpiresAt = utcPtr(expires)
	e.CreatedAt = e.CreatedAt.UTC()
	e.CreatedFrom = strPtr(createdFrom)
	e.CreatedByDevice = strPtr(device)
	e.CreatedByDeviceLabel = strPtr(label)
	return &e, nil
}

// FindWhitelist returns the unexpired entry for (username, context) on this
// server, or the global one. NULLS LAST puts the per-server row first when
// both exist, so it wins.
func (p *Postgres) FindWhitelist(ctx context.Context, username, context, serverID string, now time.Time) (*model.WhitelistEntry, error) {
	row := p.db.QueryRowContext(ctx, whitelistSelect+`
		WHERE w.username = $1 AND w.context = $2
			AND (w.server_id = $3::uuid OR w.server_id IS NULL)
			AND (w.expires_at IS NULL OR w.expires_at > $4)
		ORDER BY w.server_id NULLS LAST
		LIMIT 1`, username, context, serverID, now)
	e, err := scanWhitelist(row)
	if err != nil {
		return nil, wrap("find whitelist", err)
	}
	return e, nil
}

// ListWhitelist deletes the expired rows first (the lazy deletion of the
// contract), then lists every remaining entry, or the given server's plus
// the global ones.
func (p *Postgres) ListWhitelist(ctx context.Context, serverID *string, now time.Time) ([]model.WhitelistEntry, error) {
	_, err := p.db.ExecContext(ctx,
		`DELETE FROM whitelist WHERE expires_at IS NOT NULL AND expires_at <= $1`, now)
	if err != nil {
		return nil, wrap("delete expired whitelist", err)
	}

	q := whitelistSelect + ` WHERE (w.expires_at IS NULL OR w.expires_at > $1)`
	args := []any{now}
	if serverID != nil {
		q += ` AND (w.server_id = $2::uuid OR w.server_id IS NULL)`
		args = append(args, *serverID)
	}
	q += ` ORDER BY w.created_at DESC, w.id`

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrap("list whitelist", err)
	}
	defer rows.Close()
	out := make([]model.WhitelistEntry, 0)
	for rows.Next() {
		e, err := scanWhitelist(rows)
		if err != nil {
			return nil, wrap("list whitelist", err)
		}
		out = append(out, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list whitelist", err)
	}
	return out, nil
}

// UpsertWhitelist inserts the entry, or refreshes expires_at of the existing
// one for the same (username, context, server). The two unique indexes of
// the schema (one for per-server rows, one for global rows) need two ON
// CONFLICT targets, hence two statements. Only expires_at changes on a
// conflict: created_at, created_from and created_by_device keep telling
// where the entry came from. xmax = 0 is how PostgreSQL tells a fresh insert
// from an updated row.
func (p *Postgres) UpsertWhitelist(ctx context.Context, e *model.WhitelistEntry, now time.Time) (*model.WhitelistEntry, bool, error) {
	var row *sql.Row
	if e.ServerID != nil {
		row = p.db.QueryRowContext(ctx, `
			INSERT INTO whitelist (username, context, server_id, expires_at, created_at, created_from, created_by_device)
			VALUES ($1, $2, $3::uuid, $4, $5, $6::uuid, $7::uuid)
			ON CONFLICT (username, context, server_id) WHERE server_id IS NOT NULL
			DO UPDATE SET expires_at = EXCLUDED.expires_at
			RETURNING id::text, (xmax = 0) AS inserted`,
			e.Username, e.Context, *e.ServerID, e.ExpiresAt, now, e.CreatedFrom, e.CreatedByDevice)
	} else {
		row = p.db.QueryRowContext(ctx, `
			INSERT INTO whitelist (username, context, server_id, expires_at, created_at, created_from, created_by_device)
			VALUES ($1, $2, NULL, $3, $4, $5::uuid, $6::uuid)
			ON CONFLICT (username, context) WHERE server_id IS NULL
			DO UPDATE SET expires_at = EXCLUDED.expires_at
			RETURNING id::text, (xmax = 0) AS inserted`,
			e.Username, e.Context, e.ExpiresAt, now, e.CreatedFrom, e.CreatedByDevice)
	}
	var id string
	var inserted bool
	if err := row.Scan(&id, &inserted); err != nil {
		return nil, false, wrap("upsert whitelist", err) // unknown server: foreign key, ErrNotFound
	}
	entry, err := p.WhitelistByID(ctx, id)
	if err != nil {
		return nil, false, err
	}
	return entry, inserted, nil
}

func (p *Postgres) WhitelistByID(ctx context.Context, id string) (*model.WhitelistEntry, error) {
	row := p.db.QueryRowContext(ctx, whitelistSelect+` WHERE w.id = $1::uuid`, id)
	e, err := scanWhitelist(row)
	if err != nil {
		return nil, wrap("whitelist by id", err)
	}
	return e, nil
}

func (p *Postgres) DeleteWhitelist(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM whitelist WHERE id = $1::uuid`, id)
	return affected("delete whitelist", res, err)
}
