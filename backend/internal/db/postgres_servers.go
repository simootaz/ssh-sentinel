package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// UUIDs travel as strings: selected with ::text, bound with ::uuid, so that
// the model needs no UUID type and the database still validates the format.
const serverCols = `id::text, name, token_hash, os, created_at, last_seen_at`

func scanServer(row scanner) (*model.Server, error) {
	var s model.Server
	var lastSeen sql.NullTime
	if err := row.Scan(&s.ID, &s.Name, &s.TokenHash, &s.OS, &s.CreatedAt, &lastSeen); err != nil {
		return nil, err
	}
	s.CreatedAt = s.CreatedAt.UTC()
	s.LastSeenAt = utcPtr(lastSeen)
	return &s, nil
}

func (p *Postgres) CreateServer(ctx context.Context, name, tokenHash, os string, now time.Time) (*model.Server, error) {
	row := p.db.QueryRowContext(ctx, `
		INSERT INTO servers (name, token_hash, os, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING `+serverCols, name, tokenHash, os, now)
	s, err := scanServer(row)
	if err != nil {
		return nil, wrap("create server", err)
	}
	return s, nil
}

func (p *Postgres) ServerByTokenHash(ctx context.Context, hash string) (*model.Server, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE token_hash = $1`, hash)
	s, err := scanServer(row)
	if err != nil {
		return nil, wrap("server by token hash", err)
	}
	return s, nil
}

func (p *Postgres) ServerByName(ctx context.Context, name string) (*model.Server, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE name = $1`, name)
	s, err := scanServer(row)
	if err != nil {
		return nil, wrap("server by name", err)
	}
	return s, nil
}

func (p *Postgres) TouchServer(ctx context.Context, id string, now time.Time) error {
	res, err := p.db.ExecContext(ctx, `UPDATE servers SET last_seen_at = $2 WHERE id = $1::uuid`, id, now)
	return affected("touch server", res, err)
}
