package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/simootaz/ssh-sentinel/backend/migrations"
)

// migrateLockKey is the advisory lock every migration step takes, so that
// several container replicas starting at the same time apply the files one
// after the other instead of racing. Any 64-bit value works; it only has to
// be the same in every replica and unused by anything else on the database.
const migrateLockKey int64 = 7351924608

// Migrate applies the embedded SQL files that have not been applied yet, in
// file name order, and records each one in schema_migrations under its name
// without the .sql suffix. Running it again is a no-op. Each file runs in
// its own transaction together with its version row, so a failing file
// leaves no half-applied schema behind.
func (p *Postgres) Migrate(ctx context.Context) error {
	err := p.migrationStep(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS schema_migrations (
				version    TEXT PRIMARY KEY,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`)
		return err
	})
	if err != nil {
		return fmt.Errorf("db: migrate: schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("db: migrate: read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if err := p.applyMigration(ctx, name); err != nil {
			return fmt.Errorf("db: migrate %s: %w", name, err)
		}
	}
	return nil
}

// applyMigration runs one file unless its version is already recorded. The
// check and the insert happen under the advisory lock, so two replicas
// cannot both decide that the file is new.
func (p *Postgres) applyMigration(ctx context.Context, name string) error {
	version := strings.TrimSuffix(name, ".sql")
	return p.migrationStep(ctx, func(tx *sql.Tx) error {
		var applied bool
		err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check version: %w", err)
		}
		if applied {
			return nil
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		// One Exec for the whole file: without bound parameters the driver
		// uses the simple protocol, which accepts several statements.
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			return fmt.Errorf("record version: %w", err)
		}
		return nil
	})
}

// migrationStep runs fn in a transaction that holds the migration lock. The
// lock is released with the transaction, whether it commits or rolls back.
func (p *Postgres) migrationStep(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // no-op once committed

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("lock: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
