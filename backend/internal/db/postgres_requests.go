package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// requestSelect is the SELECT every request query shares: the row plus the
// server name and the device label, joined for display. LEFT JOIN so that a
// request whose device was deleted (decided_by_device set to NULL) still
// comes back. host() gives the plain address of the contract; a ::text cast
// would append the netmask ("203.0.113.42/32").
const requestSelect = `
	SELECT r.id::text, r.server_id::text, COALESCE(s.name, ''), r.context, r.username,
		host(r.source_ip), r.hostname, r.tty, r.command, r.geo, r.status,
		r.decided_by, r.decided_by_device::text, d.label,
		r.created_at, r.expires_at, r.decided_at
	FROM requests r
	LEFT JOIN servers s ON s.id = r.server_id
	LEFT JOIN devices d ON d.id = r.decided_by_device`

func scanRequest(row scanner) (*model.Request, error) {
	var r model.Request
	var sourceIP, tty, command, decidedBy, device, label sql.NullString
	var geo []byte
	var decidedAt sql.NullTime
	err := row.Scan(&r.ID, &r.ServerID, &r.ServerName, &r.Context, &r.Username,
		&sourceIP, &r.Hostname, &tty, &command, &geo, &r.Status,
		&decidedBy, &device, &label,
		&r.CreatedAt, &r.ExpiresAt, &decidedAt)
	if err != nil {
		return nil, err
	}
	r.SourceIP = strPtr(sourceIP)
	r.TTY = strPtr(tty)
	r.Command = strPtr(command)
	r.DecidedBy = strPtr(decidedBy)
	r.DecidedByDevice = strPtr(device)
	r.DecidedByDeviceLabel = strPtr(label)
	if r.Geo, err = geoFromJSON(geo); err != nil {
		return nil, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.ExpiresAt = r.ExpiresAt.UTC()
	r.DecidedAt = utcPtr(decidedAt)
	return &r, nil
}

// CreateRequest inserts r as given: the handler decides the status and the
// decision columns (a whitelisted or blocked request is final from the
// start). An empty status falls back to pending, like the column default.
// The row is read back with the joins so that ServerName is filled in.
func (p *Postgres) CreateRequest(ctx context.Context, r *model.Request) (*model.Request, error) {
	status := r.Status
	if status == "" {
		status = model.StatusPending
	}
	geo, err := geoJSON(r.Geo)
	if err != nil {
		return nil, err
	}
	var id string
	err = p.db.QueryRowContext(ctx, `
		INSERT INTO requests (server_id, context, username, source_ip, hostname, tty, command, geo,
			status, decided_by, decided_by_device, created_at, expires_at, decided_at)
		VALUES ($1::uuid, $2, $3, $4::inet, $5, $6, $7, $8::jsonb,
			$9, $10, $11::uuid, $12, $13, $14)
		RETURNING id::text`,
		r.ServerID, r.Context, r.Username, r.SourceIP, r.Hostname, r.TTY, r.Command, geo,
		status, r.DecidedBy, r.DecidedByDevice, r.CreatedAt, r.ExpiresAt, r.DecidedAt).Scan(&id)
	if err != nil {
		return nil, wrap("create request", err)
	}
	return p.RequestByID(ctx, id)
}

func (p *Postgres) RequestByID(ctx context.Context, id string) (*model.Request, error) {
	row := p.db.QueryRowContext(ctx, requestSelect+` WHERE r.id = $1::uuid`, id)
	r, err := scanRequest(row)
	if err != nil {
		return nil, wrap("request by id", err)
	}
	return r, nil
}

// DecideRequest is the "first verdict wins" update: only a pending row that
// has not expired yet is changed, whichever phone gets there first.
func (p *Postgres) DecideRequest(ctx context.Context, id, status string, deviceID *string, now time.Time) (*model.Request, error) {
	res, err := p.db.ExecContext(ctx, `
		UPDATE requests
		SET status = $2, decided_by = $3, decided_by_device = $4::uuid, decided_at = $5
		WHERE id = $1::uuid AND status = $6 AND expires_at > $5`,
		id, status, model.DecidedByAdmin, deviceID, now, model.StatusPending)
	if err != nil {
		return nil, wrap("decide request", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("db: decide request: %w", err)
	}
	if n == 0 {
		// Either the id is unknown, or the row is no longer pending: decided
		// by another phone, timed out, or past its expiry. Look it up to tell.
		if _, err := p.RequestByID(ctx, id); err != nil {
			return nil, err
		}
		return nil, ErrAlreadyDecided
	}
	return p.RequestByID(ctx, id)
}

// MarkTimeout closes a pending request nobody answered. decided_at stays
// NULL: a timeout has no decision instant, so now is not stored.
func (p *Postgres) MarkTimeout(ctx context.Context, id string, now time.Time) (bool, error) {
	res, err := p.db.ExecContext(ctx, `
		UPDATE requests SET status = $2, decided_by = $3, decided_at = NULL
		WHERE id = $1::uuid AND status = $4`,
		id, model.StatusTimeout, model.DecidedByTimeout, model.StatusPending)
	if err != nil {
		return false, wrap("mark timeout", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("db: mark timeout: %w", err)
	}
	if n == 0 {
		// Not pending any more, or unknown: only the second one is an error.
		if _, err := p.RequestByID(ctx, id); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// ListRequests builds the WHERE clause from the filters that are set. The
// parameters are numbered as they are appended, so the SQL and the values
// cannot drift apart.
func (p *Postgres) ListRequests(ctx context.Context, f RequestFilter) ([]model.Request, error) {
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Before != nil {
		add("r.created_at < $%d", *f.Before)
	}
	if f.Server != "" {
		add("s.name = $%d", f.Server)
	}
	if f.Username != "" {
		add("r.username = $%d", f.Username)
	}
	if f.Context != "" {
		add("r.context = $%d", f.Context)
	}
	if f.Status != "" {
		add("r.status = $%d", f.Status)
	}

	q := requestSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY r.created_at DESC, r.id DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}

	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, wrap("list requests", err)
	}
	defer rows.Close()
	out := make([]model.Request, 0)
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, wrap("list requests", err)
		}
		out = append(out, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list requests", err)
	}
	return out, nil
}

// CountDenials feeds the auto-block: admin denials only (status denied),
// from this address, decided at or after since.
func (p *Postgres) CountDenials(ctx context.Context, ip string, since time.Time) (DenialStats, error) {
	var st DenialStats
	var first, last sql.NullTime
	err := p.db.QueryRowContext(ctx, `
		SELECT count(*), min(decided_at), max(decided_at)
		FROM requests
		WHERE status = $3 AND source_ip = $1::inet AND decided_at >= $2`,
		ip, since, model.StatusDenied).Scan(&st.Count, &first, &last)
	if err != nil {
		return DenialStats{}, wrap("count denials", err)
	}
	if first.Valid {
		st.First = first.Time.UTC()
	}
	if last.Valid {
		st.Last = last.Time.UTC()
	}
	return st, nil
}
