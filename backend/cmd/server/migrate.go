package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
)

// migrateTimeout bounds the whole run: connecting plus every pending file.
const migrateTimeout = 2 * time.Minute

// runMigrate applies the pending migrations and exits. The Scaleway deploy
// runs it as a step before the function goes live; the container runs the
// same code on start. Needs DATABASE_URL only.
func runMigrate(getenv func(string) string, stdout, stderr io.Writer) int {
	url := getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(stderr, "migrate: DATABASE_URL is not set")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	pg, err := db.Open(ctx, url)
	if err != nil {
		fmt.Fprintln(stderr, "migrate: open database:", err)
		return 1
	}
	defer pg.Close()

	if err := pg.Migrate(ctx); err != nil {
		fmt.Fprintln(stderr, "migrate:", err)
		return 1
	}
	fmt.Fprintln(stdout, "migrations applied")
	return 0
}
