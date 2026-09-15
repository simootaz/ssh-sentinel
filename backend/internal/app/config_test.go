package app

import (
	"strings"
	"testing"
	"time"
)

// envOf turns a map into the getenv function LoadConfig expects.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// minimal is the smallest valid environment: the two required variables.
func minimal() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://postgres:dev@localhost:5432/postgres?sslmode=disable",
		"ADMIN_TOKEN":  "dev-admin-token",
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(envOf(minimal()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.VerdictWait != 25*time.Second {
		t.Errorf("VerdictWait = %v, want 25s", cfg.VerdictWait)
	}
	if cfg.AutoblockThreshold != 3 {
		t.Errorf("AutoblockThreshold = %d, want 3", cfg.AutoblockThreshold)
	}
	if cfg.AutoblockWindow != time.Hour {
		t.Errorf("AutoblockWindow = %v, want 1h", cfg.AutoblockWindow)
	}
	if cfg.AutoblockDuration != 0 {
		t.Errorf("AutoblockDuration = %v, want 0", cfg.AutoblockDuration)
	}
	if cfg.GeoLookupURL != "" {
		t.Errorf("GeoLookupURL = %q, want empty", cfg.GeoLookupURL)
	}
	if cfg.PushEnabled() {
		t.Error("PushEnabled() = true with no FCM variables, want false")
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	env := minimal()
	env["LISTEN_ADDR"] = "127.0.0.1:9090"
	env["VERDICT_WAIT_SECONDS"] = "10"
	env["AUTOBLOCK_THRESHOLD"] = "5"
	env["AUTOBLOCK_WINDOW_SECONDS"] = "600"
	env["AUTOBLOCK_DURATION_SECONDS"] = "86400"
	env["GEO_LOOKUP_URL"] = "http://127.0.0.1:9000/{ip}.json"
	env["FCM_PROJECT_ID"] = "my-project"
	env["FCM_SERVICE_ACCOUNT_JSON"] = `{"type": "service_account"}`

	cfg, err := LoadConfig(envOf(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.VerdictWait != 10*time.Second {
		t.Errorf("VerdictWait = %v", cfg.VerdictWait)
	}
	if cfg.AutoblockThreshold != 5 {
		t.Errorf("AutoblockThreshold = %d", cfg.AutoblockThreshold)
	}
	if cfg.AutoblockWindow != 10*time.Minute {
		t.Errorf("AutoblockWindow = %v", cfg.AutoblockWindow)
	}
	if cfg.AutoblockDuration != 24*time.Hour {
		t.Errorf("AutoblockDuration = %v", cfg.AutoblockDuration)
	}
	if cfg.GeoLookupURL != "http://127.0.0.1:9000/{ip}.json" {
		t.Errorf("GeoLookupURL = %q", cfg.GeoLookupURL)
	}
	if !cfg.PushEnabled() {
		t.Error("PushEnabled() = false with both FCM variables set, want true")
	}
}

func TestLoadConfigTrimsWhitespace(t *testing.T) {
	env := minimal()
	env["ADMIN_TOKEN"] = "  dev-admin-token\n"
	env["LISTEN_ADDR"] = " :9000 "
	cfg, err := LoadConfig(envOf(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AdminToken != "dev-admin-token" {
		t.Errorf("AdminToken = %q, want trimmed", cfg.AdminToken)
	}
	if cfg.ListenAddr != ":9000" {
		t.Errorf("ListenAddr = %q, want trimmed", cfg.ListenAddr)
	}
}

func TestLoadConfigRequiredVariables(t *testing.T) {
	_, err := LoadConfig(envOf(map[string]string{}))
	if err == nil {
		t.Fatal("expected an error with an empty environment")
	}
	for _, want := range []string{"DATABASE_URL", "ADMIN_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}

	env := minimal()
	delete(env, "ADMIN_TOKEN")
	_, err = LoadConfig(envOf(env))
	if err == nil || !strings.Contains(err.Error(), "ADMIN_TOKEN") {
		t.Errorf("missing ADMIN_TOKEN: got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("DATABASE_URL was set, error should not mention it: %v", err)
	}
}

func TestLoadConfigIntegerErrors(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"AUTOBLOCK_THRESHOLD", "abc"},
		{"AUTOBLOCK_THRESHOLD", "0"},
		{"AUTOBLOCK_WINDOW_SECONDS", "1h"},
		{"AUTOBLOCK_WINDOW_SECONDS", "-1"},
		{"AUTOBLOCK_DURATION_SECONDS", "-5"},
		{"AUTOBLOCK_DURATION_SECONDS", "1.5"},
		{"VERDICT_WAIT_SECONDS", "twenty"},
	}
	for _, c := range cases {
		env := minimal()
		env[c.name] = c.value
		_, err := LoadConfig(envOf(env))
		if err == nil {
			t.Errorf("%s=%q: expected an error", c.name, c.value)
			continue
		}
		if !strings.Contains(err.Error(), c.name) {
			t.Errorf("%s=%q: error %q does not name the variable", c.name, c.value, err)
		}
	}
}

func TestLoadConfigVerdictWaitRange(t *testing.T) {
	for _, bad := range []string{"0", "-1", "56", "120"} {
		env := minimal()
		env["VERDICT_WAIT_SECONDS"] = bad
		if _, err := LoadConfig(envOf(env)); err == nil {
			t.Errorf("VERDICT_WAIT_SECONDS=%s: expected an error", bad)
		}
	}
	for _, good := range []string{"1", "25", "55"} {
		env := minimal()
		env["VERDICT_WAIT_SECONDS"] = good
		if _, err := LoadConfig(envOf(env)); err != nil {
			t.Errorf("VERDICT_WAIT_SECONDS=%s: unexpected error %v", good, err)
		}
	}
}

func TestLoadConfigFCMBothOrNeither(t *testing.T) {
	env := minimal()
	env["FCM_PROJECT_ID"] = "my-project"
	if _, err := LoadConfig(envOf(env)); err == nil || !strings.Contains(err.Error(), "FCM_SERVICE_ACCOUNT_JSON") {
		t.Errorf("project id without key: got %v", err)
	}

	env = minimal()
	env["FCM_SERVICE_ACCOUNT_JSON"] = `{"type": "service_account"}`
	if _, err := LoadConfig(envOf(env)); err == nil || !strings.Contains(err.Error(), "FCM_PROJECT_ID") {
		t.Errorf("key without project id: got %v", err)
	}

	env = minimal()
	env["FCM_PROJECT_ID"] = ""
	env["FCM_SERVICE_ACCOUNT_JSON"] = ""
	cfg, err := LoadConfig(envOf(env))
	if err != nil {
		t.Fatalf("both empty: unexpected error %v", err)
	}
	if cfg.PushEnabled() {
		t.Error("both empty: PushEnabled() = true, want false")
	}
}

func TestLoadConfigReportsEveryProblem(t *testing.T) {
	env := map[string]string{
		"ADMIN_TOKEN":          "x",
		"VERDICT_WAIT_SECONDS": "99",
		"AUTOBLOCK_THRESHOLD":  "many",
	}
	_, err := LoadConfig(envOf(env))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"DATABASE_URL", "VERDICT_WAIT_SECONDS", "AUTOBLOCK_THRESHOLD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
