package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
)

// The PostgreSQL tests need a database, given as TEST_DATABASE_URL (see the
// README). Each store gets a schema of its own, created empty, migrated,
// and dropped at the end, so the tests can run against any database
// without touching what is already there.

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return url
}

// openTestStore opens a store on a fresh private schema and migrates it.
func openTestStore(t *testing.T) *db.Postgres {
	t.Helper()
	ctx := context.Background()
	p, err := db.Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("random schema name: %v", err)
	}
	schema := "sentinel_test_" + hex.EncodeToString(suffix[:])

	// One connection only, so that SET search_path applies to every query
	// the store runs, migrations included.
	p.DB().SetMaxOpenConns(1)
	if _, err := p.DB().ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		p.Close()
		t.Fatalf("create schema: %v", err)
	}
	if _, err := p.DB().ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		p.Close()
		t.Fatalf("set search_path: %v", err)
	}
	t.Cleanup(func() {
		if _, err := p.DB().ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		p.Close()
	})

	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return p
}

func TestPostgresConformance(t *testing.T) {
	testDatabaseURL(t)
	dbtest.Conformance(t, func(t *testing.T) db.Store { return openTestStore(t) })
}

// Open must fail fast when nothing listens, not hang: the container start
// and GET /healthz both depend on it.
func TestOpenUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	p, err := db.Open(ctx, "postgres://x:y@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1")
	if err == nil {
		p.Close()
		t.Fatal("open: expected an error for an unreachable database")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("open took %v to fail, want a quick error", took)
	}
}
