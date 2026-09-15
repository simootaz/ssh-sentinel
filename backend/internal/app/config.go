// Package app reads the configuration from the environment and wires the
// packages of the backend into one http.Handler. cmd/server (the container)
// and adapters/scaleway/api (the Scaleway function) both call LoadConfig
// then New, so the two deployments read the same variables and build the
// same handler.
package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Defaults of the optional variables (README.md, Configuration).
const (
	DefaultListenAddr         = ":8080"
	DefaultVerdictWait        = 25 * time.Second
	DefaultAutoblockThreshold = 3
	DefaultAutoblockWindow    = time.Hour
	DefaultAutoblockDuration  = 0 // until unblocked from the app

	// The wait must end before the agent's own 30 s budget (5 s margin by
	// default) and, whatever the agent is set to, before Scaleway's 60 s
	// function timeout.
	minVerdictWaitSeconds = 1
	maxVerdictWaitSeconds = 55
)

// Config is the backend's configuration. Every field comes from the
// environment variable named in the comment.
type Config struct {
	DatabaseURL           string // DATABASE_URL, required
	AdminToken            string // ADMIN_TOKEN, required
	FCMProjectID          string // FCM_PROJECT_ID
	FCMServiceAccountJSON string // FCM_SERVICE_ACCOUNT_JSON, the content of the key file
	ListenAddr            string // LISTEN_ADDR, container only
	GeoLookupURL          string // GEO_LOOKUP_URL, empty disables the geo lookup

	VerdictWait       time.Duration // VERDICT_WAIT_SECONDS
	AutoblockWindow   time.Duration // AUTOBLOCK_WINDOW_SECONDS
	AutoblockDuration time.Duration // AUTOBLOCK_DURATION_SECONDS, 0 = until unblocked

	AutoblockThreshold int // AUTOBLOCK_THRESHOLD
}

// PushEnabled reports whether FCM is configured. Both FCM variables empty
// means "push disabled": requests are stored and answered, but no phone is
// notified. That is only meant for local smoke tests, where curl plays the
// phone (TESTING.md). A real deployment sets both.
func (c Config) PushEnabled() bool {
	return c.FCMProjectID != "" && c.FCMServiceAccountJSON != ""
}

// LoadConfig reads the variables through getenv (os.Getenv in production, a
// map in tests), applies the defaults and validates. Every problem is
// reported in one error, so that a deployment is fixed in one round rather
// than one variable per restart.
func LoadConfig(getenv func(string) string) (Config, error) {
	// Values pasted into a .env file or a secret manager often carry a
	// trailing newline or space; none of our variables legitimately does.
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }

	var problems []string

	cfg := Config{
		DatabaseURL:           get("DATABASE_URL"),
		AdminToken:            get("ADMIN_TOKEN"),
		FCMProjectID:          get("FCM_PROJECT_ID"),
		FCMServiceAccountJSON: get("FCM_SERVICE_ACCOUNT_JSON"),
		ListenAddr:            get("LISTEN_ADDR"),
		GeoLookupURL:          get("GEO_LOOKUP_URL"),
	}

	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	if cfg.AdminToken == "" {
		problems = append(problems, "ADMIN_TOKEN is required")
	}
	if (cfg.FCMProjectID == "") != (cfg.FCMServiceAccountJSON == "") {
		problems = append(problems, "FCM_PROJECT_ID and FCM_SERVICE_ACCOUNT_JSON must be set together (both empty disables push, for local smoke tests only)")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = DefaultListenAddr
	}

	// intVar reads an integer variable with a default and bounds. On a
	// problem it records the message and returns the default, so that the
	// remaining variables are still checked.
	intVar := func(name string, def, lo, hi int) int {
		s := get(name)
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s must be an integer, got %q", name, s))
			return def
		}
		if n < lo {
			problems = append(problems, fmt.Sprintf("%s must be at least %d, got %d", name, lo, n))
			return def
		}
		if hi > 0 && n > hi {
			problems = append(problems, fmt.Sprintf("%s must be at most %d, got %d", name, hi, n))
			return def
		}
		return n
	}

	cfg.VerdictWait = time.Duration(intVar("VERDICT_WAIT_SECONDS", int(DefaultVerdictWait/time.Second), minVerdictWaitSeconds, maxVerdictWaitSeconds)) * time.Second
	cfg.AutoblockThreshold = intVar("AUTOBLOCK_THRESHOLD", DefaultAutoblockThreshold, 1, 0)
	cfg.AutoblockWindow = time.Duration(intVar("AUTOBLOCK_WINDOW_SECONDS", int(DefaultAutoblockWindow/time.Second), 1, 0)) * time.Second
	cfg.AutoblockDuration = time.Duration(intVar("AUTOBLOCK_DURATION_SECONDS", int(DefaultAutoblockDuration/time.Second), 0, 0)) * time.Second

	if len(problems) > 0 {
		return Config{}, errors.New("configuration: " + strings.Join(problems, "; "))
	}
	return cfg, nil
}
