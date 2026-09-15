package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// ip is INET; host() returns the plain address ("203.0.113.42"), which is
// what the contract shows and what the agent sent. A ::text cast would
// append the netmask ("203.0.113.42/32").
const blockedIPCols = `id::text, host(ip), reason, denial_count, first_denied_at, last_denied_at,
	hit_count, last_hit_at, created_at, expires_at`

func scanBlockedIP(row scanner) (*model.BlockedIP, error) {
	var b model.BlockedIP
	var lastHit, expires sql.NullTime
	if err := row.Scan(&b.ID, &b.IP, &b.Reason, &b.DenialCount, &b.FirstDeniedAt, &b.LastDeniedAt,
		&b.HitCount, &lastHit, &b.CreatedAt, &expires); err != nil {
		return nil, err
	}
	b.FirstDeniedAt = b.FirstDeniedAt.UTC()
	b.LastDeniedAt = b.LastDeniedAt.UTC()
	b.LastHitAt = utcPtr(lastHit)
	b.CreatedAt = b.CreatedAt.UTC()
	b.ExpiresAt = utcPtr(expires)
	return &b, nil
}

// BlockedIPByIP returns the row for ip whether the block is still active or
// not: the rules need the expired row to know since when to count denials.
func (p *Postgres) BlockedIPByIP(ctx context.Context, ip string) (*model.BlockedIP, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+blockedIPCols+` FROM blocked_ips WHERE ip = $1::inet`, ip)
	b, err := scanBlockedIP(row)
	if err != nil {
		return nil, wrap("blocked ip by ip", err)
	}
	return b, nil
}

func (p *Postgres) RecordBlockedIPHit(ctx context.Context, id string, now time.Time) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE blocked_ips SET hit_count = hit_count + 1, last_hit_at = $2 WHERE id = $1::uuid`, id, now)
	return affected("record blocked ip hit", res, err)
}

func (p *Postgres) ListBlockedIPs(ctx context.Context, now time.Time) ([]model.BlockedIP, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT `+blockedIPCols+` FROM blocked_ips
		WHERE expires_at IS NULL OR expires_at > $1
		ORDER BY created_at DESC, id`, now)
	if err != nil {
		return nil, wrap("list blocked ips", err)
	}
	defer rows.Close()
	out := make([]model.BlockedIP, 0)
	for rows.Next() {
		b, err := scanBlockedIP(rows)
		if err != nil {
			return nil, wrap("list blocked ips", err)
		}
		out = append(out, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list blocked ips", err)
	}
	return out, nil
}

// UpsertBlockedIP inserts the block, or re-arms the existing row for the
// same ip: the counters and the expiry come from b, the hit counter starts
// again at zero, the id is kept so that the app keeps a stable reference.
func (p *Postgres) UpsertBlockedIP(ctx context.Context, b *model.BlockedIP) (*model.BlockedIP, error) {
	reason := b.Reason
	if reason == "" {
		reason = "autoblock" // same default as the column
	}
	row := p.db.QueryRowContext(ctx, `
		INSERT INTO blocked_ips (ip, reason, denial_count, first_denied_at, last_denied_at,
			hit_count, last_hit_at, created_at, expires_at)
		VALUES ($1::inet, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (ip) DO UPDATE SET
			reason = EXCLUDED.reason,
			denial_count = EXCLUDED.denial_count,
			first_denied_at = EXCLUDED.first_denied_at,
			last_denied_at = EXCLUDED.last_denied_at,
			hit_count = 0,
			last_hit_at = NULL,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
		RETURNING `+blockedIPCols,
		b.IP, reason, b.DenialCount, b.FirstDeniedAt, b.LastDeniedAt,
		b.HitCount, b.LastHitAt, b.CreatedAt, b.ExpiresAt)
	out, err := scanBlockedIP(row)
	if err != nil {
		return nil, wrap("upsert blocked ip", err)
	}
	return out, nil
}

// UnblockIP ends an active block by setting its expiry to now. The row stays
// so that the denials before the unblock are not counted again.
func (p *Postgres) UnblockIP(ctx context.Context, id string, now time.Time) error {
	res, err := p.db.ExecContext(ctx, `
		UPDATE blocked_ips SET expires_at = $2
		WHERE id = $1::uuid AND (expires_at IS NULL OR expires_at > $2)`, id, now)
	return affected("unblock ip", res, err)
}
