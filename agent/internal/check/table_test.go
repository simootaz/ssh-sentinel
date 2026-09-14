package check

import (
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
	"github.com/simootaz/ssh-sentinel/agent/internal/whitelist"
)

// row is one case of the decision table. setup prepares the files and returns the deps.
type row struct {
	name                      string
	setup                     func(t *testing.T, f *fixture) Deps
	code                      int
	decision, reason, request string
}

func TestRunDecisionTable(t *testing.T) {
	permanent := whitelist.Entry{Username: "deploy", Context: "ssh"}
	valid := whitelist.Entry{Username: "deploy", Context: "ssh", ExpiresAt: ptr(now.Add(time.Hour))}
	expired := whitelist.Entry{Username: "deploy", Context: "ssh", ExpiresAt: ptr(now.Add(-time.Minute))}
	sudoOnly := whitelist.Entry{Username: "deploy", Context: "sudo"}

	enforce := func(status int, body string) func(*testing.T, *fixture) Deps {
		return func(t *testing.T, f *fixture) Deps { return f.deps(backend(t, status, body)) }
	}
	notify := func(status int, body string) func(*testing.T, *fixture) Deps {
		return func(t *testing.T, f *fixture) Deps {
			d := f.deps(backend(t, status, body))
			d.Mode = config.ModeNotify
			return d
		}
	}
	cached := func(e whitelist.Entry, status int, body string) func(*testing.T, *fixture) Deps {
		return func(t *testing.T, f *fixture) Deps { f.cache(e); return f.deps(backend(t, status, body)) }
	}

	rows := []row{
		{"row 1: break-glass hit, no network", func(t *testing.T, f *fixture) Deps { f.breakGlass("deploy"); return f.deps(mustNotCall{t}) }, 0, "allow", "breakglass", ""},
		{"break-glass lists someone else", func(t *testing.T, f *fixture) Deps {
			f.breakGlass("root")
			return f.deps(backend(t, 200, approve("admin")))
		}, 0, "allow", "admin", "req-1"},
		{"break-glass file missing", enforce(200, approve("admin")), 0, "allow", "admin", "req-1"},
		{"empty user", func(t *testing.T, f *fixture) Deps { d := f.deps(mustNotCall{t}); d.User = ""; return d }, 1, "deny", "error", ""},

		{"row 2: notify, backend answers", notify(200, approve("notify")), 0, "allow", "notify", "req-1"},
		{"notify, backend down", func(t *testing.T, f *fixture) Deps {
			d := f.deps(closedBackend(t))
			d.Mode = config.ModeNotify
			return d
		}, 0, "allow", "unreachable", ""},
		{"notify, backend rejects", notify(401, `{"error":"bad token"}`), 0, "allow", "rejected", ""},
		{"notify, no config", func(t *testing.T, f *fixture) Deps { d := f.deps(nil); d.Mode = config.ModeNotify; return d }, 0, "allow", "unreachable", ""},

		{"row 3: approve by admin", enforce(200, approve("admin")), 0, "allow", "admin", "req-1"},
		{"row 3: approve by whitelist", enforce(200, approve("whitelist")), 0, "allow", "whitelist", "req-1"},
		{"row 4: deny by admin", enforce(200, deny("admin")), 1, "deny", "admin", "req-1"},
		{"row 4: deny by timeout", enforce(200, deny("timeout")), 1, "deny", "timeout", "req-1"},
		{"row 4: deny by blocked_ip", enforce(200, deny("blocked_ip")), 1, "deny", "blocked_ip", "req-1"},
		{"row 4: deny by blocked_geo", enforce(200, deny("blocked_geo")), 1, "deny", "blocked_geo", "req-1"},

		{"row 5: 401 ignores the cache", cached(permanent, 401, `{"error":"unknown server"}`), 1, "deny", "rejected", ""},
		{"row 5: 400", enforce(400, `{"error":"bad payload"}`), 1, "deny", "rejected", ""},

		{"row 6: 500 with permanent entry", cached(permanent, 500, "boom"), 0, "allow", "cache", ""},
		{"row 6: 500 with valid entry", cached(valid, 500, "boom"), 0, "allow", "cache", ""},
		{"row 6: closed backend with entry", func(t *testing.T, f *fixture) Deps { f.cache(permanent); return f.deps(closedBackend(t)) }, 0, "allow", "cache", ""},
		{"row 6: no config with entry", func(t *testing.T, f *fixture) Deps { f.cache(permanent); return f.deps(nil) }, 0, "allow", "cache", ""},
		{"row 6: sudo context with sudo entry", func(t *testing.T, f *fixture) Deps {
			f.cache(sudoOnly)
			d := f.deps(backend(t, 503, ""))
			d.Context, d.RHost = "sudo", ""
			return d
		}, 0, "allow", "cache", ""},

		{"row 7: 500 with expired entry", cached(expired, 500, "boom"), 1, "deny", "unreachable", ""},
		{"row 7: 500 with entry for the other context", cached(sudoOnly, 500, "boom"), 1, "deny", "unreachable", ""},
		{"row 7: 500 without cache", enforce(500, "boom"), 1, "deny", "unreachable", ""},
		{"row 7: closed backend without cache", func(t *testing.T, f *fixture) Deps { return f.deps(closedBackend(t)) }, 1, "deny", "unreachable", ""},
		{"row 7: malformed body", enforce(200, `{"verdict":`), 1, "deny", "unreachable", ""},
		{"row 7: unknown verdict", enforce(200, `{"verdict":"maybe"}`), 1, "deny", "unreachable", ""},
		{"row 7: no config without cache", func(t *testing.T, f *fixture) Deps { return f.deps(nil) }, 1, "deny", "unreachable", ""},

		{"row 8: panic in the backend call", func(t *testing.T, f *fixture) Deps { return f.deps(panicking{}) }, 1, "deny", "error", ""},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			d := tc.setup(t, f)
			code := Run(d)
			if code != tc.code {
				t.Errorf("exit code = %d, want %d", code, tc.code)
			}
			if len(f.rec.Lines) != 1 {
				t.Fatalf("logged %d lines, want exactly 1: %+v", len(f.rec.Lines), f.rec.Lines)
			}
			want := auditlog.Line{Decision: tc.decision, Context: d.Context, User: d.User, RHost: d.RHost, Host: "web-01", Reason: tc.reason, Request: tc.request}
			if got := f.rec.Last(); got != want {
				t.Errorf("line\n got %s\nwant %s", got, want)
			}
		})
	}
}
