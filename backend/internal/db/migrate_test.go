package db_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/simootaz/ssh-sentinel/backend/migrations"
)

func TestMigrate(t *testing.T) {
	p := openTestStore(t) // migrated once already
	ctx := context.Background()

	// A second run must change nothing and fail nothing.
	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	rows, err := p.DB().QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}

	// Every embedded file is recorded exactly once, 001_init among them.
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	var want []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			want = append(want, strings.TrimSuffix(e.Name(), ".sql"))
		}
	}
	if strings.Join(versions, ",") != strings.Join(want, ",") {
		t.Fatalf("schema_migrations = %v, want %v", versions, want)
	}
	if len(versions) == 0 || versions[0] != "001_init" {
		t.Fatalf("schema_migrations = %v, want 001_init first", versions)
	}

	// The six tables of the schema exist in the test schema.
	for _, table := range []string{"servers", "devices", "requests", "whitelist", "blocked_ips", "geo_rules"} {
		var exists bool
		err := p.DB().QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema() AND table_name = $1
			)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s missing after migrate", table)
		}
	}
}
