package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver with database/sql

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Postgres is the Store backed by database/sql and the pgx driver. The
// queries are standard SQL and live next to the method that runs them, one
// file per table (postgres_servers.go, postgres_requests.go, ...).
type Postgres struct {
	db *sql.DB
}

var _ Store = (*Postgres)(nil)

// Open connects to url (a postgres:// connection string), sets the pool
// limits and pings the database. The limits stay small on purpose: Scaleway
// Serverless SQL and a compose PostgreSQL both cap the number of
// connections, and several backend replicas share the same database.
func Open(ctx context.Context, url string) (*Postgres, error) {
	d, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	d.SetMaxOpenConns(10)
	d.SetMaxIdleConns(5)
	d.SetConnMaxLifetime(30 * time.Minute)
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	return &Postgres{db: d}, nil
}

// DB exposes the underlying pool, for the tests and the enrollment script.
func (p *Postgres) DB() *sql.DB { return p.db }

// Close closes the pool.
func (p *Postgres) Close() error { return p.db.Close() }

// Ping checks that the database answers. Used by GET /healthz.
func (p *Postgres) Ping(ctx context.Context) error {
	if err := p.db.PingContext(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}

// Error mapping. The handlers only know ErrNotFound, ErrConflict and
// ErrAlreadyDecided; everything else is a server error with the query name
// in front so that the log says which one failed.

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// wrap maps a driver error to a Store error. No rows means the row does not
// exist; a unique violation means a duplicate; a foreign key violation means
// the referenced row (a server, a device, a request) does not exist.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation:
			return ErrConflict
		case pgForeignKeyViolation:
			return ErrNotFound
		}
	}
	return fmt.Errorf("db: %s: %w", op, err)
}

// affected turns the outcome of an UPDATE or DELETE into ErrNotFound when
// no row matched.
func affected(op string, res sql.Result, err error) error {
	if err != nil {
		return wrap(op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("db: %s: %w", op, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// scanner is what *sql.Row and *sql.Rows have in common, so that one scan
// function per table serves both the single-row and the list queries.
type scanner interface {
	Scan(dest ...any) error
}

// Conversions between the nullable SQL types and the pointer fields of the
// model. Every time goes through UTC: the driver returns times in the
// session time zone, the contract renders them with a trailing Z.

func utcPtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	u := t.Time.UTC()
	return &u
}

func strPtr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

// geoJSON encodes a Geo for the JSONB column, NULL when nil.
func geoJSON(g *model.Geo) ([]byte, error) {
	if g == nil {
		return nil, nil
	}
	b, err := json.Marshal(g)
	if err != nil {
		return nil, fmt.Errorf("db: encode geo: %w", err)
	}
	return b, nil
}

// geoFromJSON decodes the JSONB column, nil when NULL.
func geoFromJSON(b []byte) (*model.Geo, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var g model.Geo
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("db: decode geo: %w", err)
	}
	return &g, nil
}
