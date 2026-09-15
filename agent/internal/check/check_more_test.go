package check

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
)

// capturing returns a client whose server decodes every request body into *last.
func capturing(t *testing.T, last *map[string]any) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		*last = body
		io.WriteString(w, approve("admin"))
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "tok")
}

func TestRunSendsTheContractBody(t *testing.T) {
	var body map[string]any

	f := newFixture(t)
	Run(f.deps(capturing(t, &body)))
	want := map[string]any{
		"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42",
		"hostname": "web-01", "tty": "ssh", "command": nil,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("ssh body\n got %v\nwant %v", body, want)
	}

	cmd := "sudo systemctl restart nginx"
	d := f.deps(capturing(t, &body))
	d.Context, d.RHost, d.TTY, d.Command = "sudo", "", "pts/0", &cmd
	Run(d)
	want = map[string]any{
		"context": "sudo", "mode": "enforce", "username": "deploy", "source_ip": nil,
		"hostname": "web-01", "tty": "pts/0", "command": cmd,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("sudo body\n got %v\nwant %v", body, want)
	}

	d = f.deps(capturing(t, &body))
	d.Mode = config.ModeNotify
	Run(d)
	if body["mode"] != "notify" {
		t.Errorf("notify mode must be sent as mode=notify, got %v", body["mode"])
	}
}

func TestRunSlowBackendDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(300 * time.Millisecond):
			io.WriteString(w, approve("admin"))
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	f := newFixture(t)
	d := f.deps(client.New(srv.URL, "tok"))
	d.Timeout = 50 * time.Millisecond
	start := time.Now()
	code := Run(d)
	if code != ExitDeny {
		t.Errorf("exit code = %d, want deny", code)
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %v, the timeout was not honoured", time.Since(start))
	}
	if got := f.rec.Last(); got.Reason != "unreachable" || got.Decision != "deny" {
		t.Errorf("line = %s", got)
	}
}

func TestRunNotifyModeCapsTheWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body) // consume the body, or the server never notices the client hanging up
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	f := newFixture(t)
	d := f.deps(client.New(srv.URL, "tok"))
	d.Mode = config.ModeNotify
	d.Timeout = 100 * time.Millisecond // min(Timeout, 10 s) keeps the test fast
	start := time.Now()
	if code := Run(d); code != ExitAllow {
		t.Errorf("notify mode must allow, got %d", code)
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	if got := f.rec.Last(); got.Reason != "unreachable" {
		t.Errorf("line = %s", got)
	}
}

func TestRunIgnoresInsecureBreakGlass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not checked on Windows")
	}
	f := newFixture(t)
	f.breakGlass("deploy")
	if err := os.Chmod(filepath.Join(f.dir, "breakglass"), 0o666); err != nil {
		t.Fatal(err)
	}
	code := Run(f.deps(backend(t, 200, deny("admin"))))
	if code != ExitDeny || f.rec.Last().Reason != "admin" {
		t.Errorf("an insecure break-glass file must be ignored: code %d, line %s", code, f.rec.Last())
	}
}

func TestRunWithMinimalDeps(t *testing.T) {
	// No clock, no logger, no client, no files: still a logged deny, never a panic.
	dir := t.TempDir()
	code := Run(Deps{Context: "ssh", User: "deploy", BreakGlassPath: filepath.Join(dir, "bg"), CachePath: filepath.Join(dir, "c")})
	if code != ExitDeny {
		t.Errorf("exit code = %d, want deny", code)
	}
}

func TestSudoCommandDoesNotPanic(t *testing.T) {
	if runtime.GOOS != "linux" {
		if SudoCommand() != nil {
			t.Error("SudoCommand must be nil outside Linux")
		}
		return
	}
	_ = SudoCommand()
}
