package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	c, err := Parse([]byte(`{"backend_url": "https://api.example.com/", "server_token": " abc "}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BackendURL != "https://api.example.com" {
		t.Errorf("BackendURL = %q, want the trailing slash removed", c.BackendURL)
	}
	if c.ServerToken != "abc" {
		t.Errorf("ServerToken = %q, want it trimmed", c.ServerToken)
	}
	if c.Mode != ModeEnforce {
		t.Errorf("Mode = %q, want %q", c.Mode, ModeEnforce)
	}
	if c.Timeout() != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", c.Timeout())
	}
	if c.WatchPoll() != 2*time.Second {
		t.Errorf("WatchPoll = %v, want 2s", c.WatchPoll())
	}
}

func TestParseEveryKey(t *testing.T) {
	c, err := Parse([]byte(`{
		"backend_url": "http://localhost:8080",
		"server_token": "abc",
		"mode": "notify",
		"timeout_seconds": 45,
		"hostname": "web-01",
		"allow_http": true,
		"watch_poll_seconds": 5
	}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Mode != ModeNotify || c.TimeoutSeconds != 45 || c.Hostname != "web-01" || !c.AllowHTTP || c.WatchPollSeconds != 5 {
		t.Errorf("unexpected config: %+v", c)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"missing url":        `{"server_token": "x"}`,
		"missing token":      `{"backend_url": "https://a"}`,
		"http without allow": `{"backend_url": "http://a", "server_token": "x"}`,
		"bad scheme":         `{"backend_url": "ftp://a", "server_token": "x"}`,
		"no host":            `{"backend_url": "https://", "server_token": "x"}`,
		"bad mode":           `{"backend_url": "https://a", "server_token": "x", "mode": "maybe"}`,
		"timeout too big":    `{"backend_url": "https://a", "server_token": "x", "timeout_seconds": 1000}`,
		"timeout negative":   `{"backend_url": "https://a", "server_token": "x", "timeout_seconds": -1}`,
		"poll zero":          `{"backend_url": "https://a", "server_token": "x", "watch_poll_seconds": -3}`,
		"unknown key":        `{"backend_url": "https://a", "server_token": "x", "backend-url": "y"}`,
		"invalid json":       `{`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Fatalf("expected an error for %s", in)
			}
		})
	}
}

func TestParseHTTPErrorExplainsItself(t *testing.T) {
	_, err := Parse([]byte(`{"backend_url": "http://a", "server_token": "x"}`))
	if err == nil || !strings.Contains(err.Error(), "allow_http") {
		t.Fatalf("error should point at allow_http, got %v", err)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := Load(path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file: got %v, want ErrNotFound", err)
	}
	if err := os.WriteFile(path, []byte(`{"backend_url": "https://a", "server_token": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BackendURL != "https://a" {
		t.Errorf("BackendURL = %q", c.BackendURL)
	}
}

func TestDefaultPathsOverride(t *testing.T) {
	t.Setenv(EnvConfigPath, "")
	base := DefaultPaths()
	if base.Config == "" || base.BreakGlass == "" || base.Cache == "" {
		t.Fatalf("empty default path: %+v", base)
	}

	lab := filepath.Join("lab", "config.json")
	t.Setenv(EnvConfigPath, lab)
	p := DefaultPaths()
	if p.Config != lab {
		t.Errorf("Config = %q, want %q", p.Config, lab)
	}
	if p.BreakGlass != base.BreakGlass || p.Cache != base.Cache {
		t.Errorf("break-glass and cache paths must not move with the override: %+v", p)
	}
}
