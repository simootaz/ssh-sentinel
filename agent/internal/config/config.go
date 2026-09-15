// Package config loads the agent configuration file (config.json).
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// ModeEnforce blocks the login until the backend answers. The default.
	ModeEnforce = "enforce"
	// ModeNotify reports the login and always allows it. Used on Windows and during rollout.
	ModeNotify = "notify"

	// EnvConfigPath overrides the config file location. Meant for lab VMs and manual runs.
	EnvConfigPath = "SSH_SENTINEL_CONFIG"

	defaultTimeoutSeconds   = 30
	maxTimeoutSeconds       = 120
	defaultWatchPollSeconds = 2
)

// ErrNotFound is returned by Load when the config file does not exist.
var ErrNotFound = errors.New("config file not found")

// Config mirrors config.json. backend_url and server_token are required, the rest has defaults.
type Config struct {
	BackendURL       string `json:"backend_url"`
	ServerToken      string `json:"server_token"`
	Mode             string `json:"mode"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	Hostname         string `json:"hostname"`
	AllowHTTP        bool   `json:"allow_http"`
	WatchPollSeconds int    `json:"watch_poll_seconds"`
}

// Timeout is the whole budget of one access request, connection included.
func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutSeconds) * time.Second }

// WatchPoll is how often the Windows watcher reads the event log.
func (c Config) WatchPoll() time.Duration { return time.Duration(c.WatchPollSeconds) * time.Second }

// Paths are the file locations of the agent on this OS.
type Paths struct {
	Config     string
	BreakGlass string
	Cache      string
}

// DefaultPaths returns the documented locations. SSH_SENTINEL_CONFIG overrides the config file
// only: the break-glass file and the cache stay at their fixed paths, so a missing or broken
// config never disables them.
func DefaultPaths() Paths {
	var p Paths
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		dir := filepath.Join(base, "ssh-sentinel")
		p = Paths{
			Config:     filepath.Join(dir, "config.json"),
			BreakGlass: filepath.Join(dir, "breakglass"),
			Cache:      filepath.Join(dir, "whitelist.json"),
		}
	} else {
		p = Paths{
			Config:     "/etc/ssh-sentinel/config.json",
			BreakGlass: "/etc/ssh-sentinel/breakglass",
			Cache:      "/var/lib/ssh-sentinel/whitelist.json",
		}
	}
	if v := os.Getenv(EnvConfigPath); v != "" {
		p.Config = v
	}
	return p
}

// Load reads and validates the config file at path.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes the JSON, fills in defaults and validates. Unknown keys are an error, so a
// typo in the file is caught at install time instead of being silently ignored.
func Parse(data []byte) (Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if err := c.finish(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c *Config) finish() error {
	c.BackendURL = strings.TrimRight(strings.TrimSpace(c.BackendURL), "/")
	c.ServerToken = strings.TrimSpace(c.ServerToken)
	if c.Mode == "" {
		c.Mode = ModeEnforce
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = defaultTimeoutSeconds
	}
	if c.WatchPollSeconds == 0 {
		c.WatchPollSeconds = defaultWatchPollSeconds
	}

	if c.BackendURL == "" {
		return errors.New("backend_url is required")
	}
	u, err := url.Parse(c.BackendURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("backend_url is not a valid URL: %q", c.BackendURL)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !c.AllowHTTP {
			return errors.New("backend_url uses http; the contract requires https (set allow_http only for a lab backend)")
		}
	default:
		return fmt.Errorf("backend_url scheme must be https, got %q", u.Scheme)
	}
	if c.ServerToken == "" {
		return errors.New("server_token is required")
	}
	if c.Mode != ModeEnforce && c.Mode != ModeNotify {
		return fmt.Errorf("mode must be %q or %q, got %q", ModeEnforce, ModeNotify, c.Mode)
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between 1 and %d", maxTimeoutSeconds)
	}
	if c.WatchPollSeconds < 1 {
		return errors.New("watch_poll_seconds must be at least 1")
	}
	return nil
}
