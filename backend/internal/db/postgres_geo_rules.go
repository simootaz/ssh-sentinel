package db

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// country is CHAR(2); the ::text cast drops the padding a CHAR column would
// otherwise add, and the scan trims for good measure.
const geoRuleCols = `id::text, country::text, note, created_at`

func scanGeoRule(row scanner) (*model.GeoRule, error) {
	var g model.GeoRule
	var note sql.NullString
	if err := row.Scan(&g.ID, &g.Country, &note, &g.CreatedAt); err != nil {
		return nil, err
	}
	g.Country = strings.TrimSpace(g.Country)
	g.Note = strPtr(note)
	g.CreatedAt = g.CreatedAt.UTC()
	return &g, nil
}

func (p *Postgres) ListGeoRules(ctx context.Context) ([]model.GeoRule, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+geoRuleCols+` FROM geo_rules ORDER BY country`)
	if err != nil {
		return nil, wrap("list geo rules", err)
	}
	defer rows.Close()
	out := make([]model.GeoRule, 0)
	for rows.Next() {
		g, err := scanGeoRule(rows)
		if err != nil {
			return nil, wrap("list geo rules", err)
		}
		out = append(out, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("list geo rules", err)
	}
	return out, nil
}

func (p *Postgres) GeoRuleByCountry(ctx context.Context, country string) (*model.GeoRule, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+geoRuleCols+` FROM geo_rules WHERE country = $1`, country)
	g, err := scanGeoRule(row)
	if err != nil {
		return nil, wrap("geo rule by country", err)
	}
	return g, nil
}

// CreateGeoRule stores country as given: the handler already validated and
// upper-cased it. A second rule for the same country is ErrConflict.
func (p *Postgres) CreateGeoRule(ctx context.Context, country string, note *string, now time.Time) (*model.GeoRule, error) {
	row := p.db.QueryRowContext(ctx, `
		INSERT INTO geo_rules (country, note, created_at)
		VALUES ($1, $2, $3)
		RETURNING `+geoRuleCols, country, note, now)
	g, err := scanGeoRule(row)
	if err != nil {
		return nil, wrap("create geo rule", err)
	}
	return g, nil
}

func (p *Postgres) DeleteGeoRule(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM geo_rules WHERE id = $1::uuid`, id)
	return affected("delete geo rule", res, err)
}
