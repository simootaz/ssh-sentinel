package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

const deviceCols = `id::text, fcm_token, platform, label, created_at, last_seen_at, deleted_at`

func scanDevice(row scanner, extra ...any) (*model.Device, error) {
	var d model.Device
	var label sql.NullString
	var lastSeen, deleted sql.NullTime
	dest := append([]any{&d.ID, &d.FCMToken, &d.Platform, &label, &d.CreatedAt, &lastSeen, &deleted}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	d.Label = strPtr(label)
	d.CreatedAt = d.CreatedAt.UTC()
	d.LastSeenAt = utcPtr(lastSeen)
	d.DeletedAt = utcPtr(deleted)
	return &d, nil
}

func (p *Postgres) UpsertDevice(ctx context.Context, fcmToken, platform string, label *string, now time.Time) (*model.Device, bool, error) {
	// xmax = 0 is how PostgreSQL tells a fresh insert from an updated row: a
	// row rewritten by ON CONFLICT DO UPDATE carries the id of the
	// transaction that replaced it, a new row carries none.
	row := p.db.QueryRowContext(ctx, `
		INSERT INTO devices (fcm_token, platform, label, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $4)
		ON CONFLICT (fcm_token) DO UPDATE
		SET platform = EXCLUDED.platform, label = EXCLUDED.label, last_seen_at = EXCLUDED.last_seen_at,
		    deleted_at = NULL
		RETURNING `+deviceCols+`, (xmax = 0) AS inserted`, fcmToken, platform, label, now)
	var inserted bool
	d, err := scanDevice(row, &inserted)
	if err != nil {
		return nil, false, wrap("upsert device", err)
	}
	return d, inserted, nil
}

func (p *Postgres) DeviceByID(ctx context.Context, id string) (*model.Device, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE id = $1::uuid AND deleted_at IS NULL`, id)
	d, err := scanDevice(row)
	if err != nil {
		return nil, wrap("device by id", err)
	}
	return d, nil
}

func (p *Postgres) ListDevices(ctx context.Context) ([]model.Device, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+deviceCols+` FROM devices WHERE deleted_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, wrap("list devices", err)
	}
	defer rows.Close()
	out := make([]model.Device, 0)
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, wrap("list devices", err)
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list devices", err)
	}
	return out, nil
}

// DeleteDevice retires the phone: deleted_at is set and the row stays, so
// the requests and whitelist entries that reference it keep showing its
// label (contract v1, DELETE /devices). Listing and pushing skip such rows.
func (p *Postgres) DeleteDevice(ctx context.Context, id string, now time.Time) error {
	res, err := p.db.ExecContext(ctx,
		`UPDATE devices SET deleted_at = $2 WHERE id = $1::uuid AND deleted_at IS NULL`, id, now)
	return affected("delete device", res, err)
}
