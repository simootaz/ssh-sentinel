package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simootaz/ssh-sentinel/agent/internal/config"
)

func TestRunDispatch(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage:") {
		t.Errorf("no arguments: code %d, stderr %q", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := run([]string{"bogus"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), `unknown command "bogus"`) {
		t.Errorf("unknown command: code %d, stderr %q", code, errb.String())
	}
	out.Reset()
	if code := run([]string{"version"}, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "ssh-sentinel ") {
		t.Errorf("version: code %d, stdout %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "check [--context=ssh|sudo]") {
		t.Errorf("help: code %d, stdout %q", code, out.String())
	}
}

func TestCheckArgumentErrors(t *testing.T) {
	var errb bytes.Buffer
	if code := runCheck([]string{"--context=root"}, &errb); code != 2 || !strings.Contains(errb.String(), "ssh or sudo") {
		t.Errorf("bad context: code %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := runCheck([]string{"extra"}, &errb); code != 2 {
		t.Errorf("stray argument: code %d", code)
	}
	errb.Reset()
	if code := runCheck([]string{"--bogus"}, &errb); code != 2 {
		t.Errorf("unknown flag: code %d", code)
	}
}

func TestSyncWithoutConfig(t *testing.T) {
	t.Setenv(config.EnvConfigPath, filepath.Join(t.TempDir(), "missing.json"))
	var out, errb bytes.Buffer
	if code := runSync(nil, &out, &errb); code != 1 || !strings.Contains(errb.String(), "config file not found") {
		t.Errorf("missing config: code %d, stderr %q", code, errb.String())
	}
	errb.Reset()
	if code := runSync([]string{"x"}, &out, &errb); code != 2 {
		t.Errorf("stray argument: code %d", code)
	}
}

func TestWatchArgumentAndConfigErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runWatch([]string{"--bogus"}, &out, &errb); code != 2 {
		t.Errorf("unknown flag: code %d", code)
	}
	errb.Reset()
	t.Setenv(config.EnvConfigPath, filepath.Join(t.TempDir(), "missing.json"))
	if code := runWatch(nil, &out, &errb); code != 1 || !strings.Contains(errb.String(), "config file not found") {
		t.Errorf("missing config: code %d, stderr %q", code, errb.String())
	}

	if runtime.GOOS == "windows" {
		return // on Windows the real event log source would be used
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"backend_url": "https://a", "server_token": "x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfigPath, path)
	errb.Reset()
	if code := runWatch(nil, &out, &errb); code != 2 || !strings.Contains(errb.String(), "only supported on Windows") {
		t.Errorf("non-windows: code %d, stderr %q", code, errb.String())
	}
}
