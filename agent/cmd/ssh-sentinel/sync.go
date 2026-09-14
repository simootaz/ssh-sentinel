package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
	"github.com/simootaz/ssh-sentinel/agent/internal/whitelist"
)

// runSync refreshes the whitelist cache from GET /whitelist. The backend already scopes the
// list to this server plus the global entries, so only username, context and expiry are kept.
// On any error the previous cache file is left untouched.
func runSync(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "usage: ssh-sentinel sync")
		return 2
	}
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.Config)
	if err != nil {
		fmt.Fprintf(stderr, "sync: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout())
	defer cancel()
	items, err := client.New(cfg.BackendURL, cfg.ServerToken).Whitelist(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "sync: %v\n", err)
		return 1
	}

	entries := make([]whitelist.Entry, 0, len(items))
	for _, it := range items {
		entries = append(entries, whitelist.Entry{Username: it.Username, Context: it.Context, ExpiresAt: it.ExpiresAt})
	}
	cache := whitelist.Cache{SyncedAt: time.Now().UTC(), Entries: entries}
	if err := whitelist.SaveCache(paths.Cache, cache); err != nil {
		fmt.Fprintf(stderr, "sync: write cache: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "synced %d whitelist entries to %s\n", len(entries), paths.Cache)
	return 0
}
