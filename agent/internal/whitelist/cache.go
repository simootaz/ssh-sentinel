package whitelist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry is one cached whitelist entry. A nil ExpiresAt means permanent.
type Entry struct {
	Username  string     `json:"username"`
	Context   string     `json:"context"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// Cache is the on-disk copy of the backend whitelist for this server.
type Cache struct {
	SyncedAt time.Time `json:"synced_at"`
	Entries  []Entry   `json:"entries"`
}

// LoadCache reads the cache file. A missing file gives an empty cache and no error.
func LoadCache(path string) (Cache, error) {
	var c Cache
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, fmt.Errorf("parse cache: %w", err)
	}
	return c, nil
}

// Allows reports whether the cache holds an unexpired entry for username in context at now.
// Expiry is judged with the caller's clock, which is this server's clock.
func (c Cache) Allows(username, context string, now time.Time) bool {
	for _, e := range c.Entries {
		if e.Username != username || e.Context != context {
			continue
		}
		if e.ExpiresAt != nil && !now.Before(*e.ExpiresAt) {
			continue
		}
		return true
	}
	return false
}

// SaveCache writes the cache atomically (temp file, then rename) with owner-only permissions.
func SaveCache(path string, c Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
