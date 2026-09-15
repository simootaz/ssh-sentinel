package check

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
	"github.com/simootaz/ssh-sentinel/agent/internal/whitelist"
)

// now is the fake clock every test runs at.
var now = time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)

// fixture is one test's temp directory and log recorder.
type fixture struct {
	t   *testing.T
	dir string
	rec *auditlog.Recorder
}

func newFixture(t *testing.T) *fixture {
	return &fixture{t: t, dir: t.TempDir(), rec: &auditlog.Recorder{}}
}

func (f *fixture) breakGlass(users ...string) {
	if err := os.WriteFile(filepath.Join(f.dir, "breakglass"), []byte(strings.Join(users, "\n")+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) cache(entries ...whitelist.Entry) {
	if err := whitelist.SaveCache(filepath.Join(f.dir, "whitelist.json"), whitelist.Cache{SyncedAt: now, Entries: entries}); err != nil {
		f.t.Fatal(err)
	}
}

// deps is an ssh login by deploy from 203.0.113.42 on web-01, in enforce mode.
func (f *fixture) deps(c Requester) Deps {
	return Deps{
		Context:        "ssh",
		Mode:           config.ModeEnforce,
		User:           "deploy",
		RHost:          "203.0.113.42",
		TTY:            "ssh",
		Hostname:       "web-01",
		BreakGlassPath: filepath.Join(f.dir, "breakglass"),
		CachePath:      filepath.Join(f.dir, "whitelist.json"),
		Client:         c,
		Timeout:        2 * time.Second,
		Now:            func() time.Time { return now },
		Log:            f.rec,
	}
}

// backend is a client for an httptest server that always answers status and body.
func backend(t *testing.T, status int, body string) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return client.New(srv.URL, "tok")
}

// closedBackend is a client for a server that is already gone: connection refused.
func closedBackend(t *testing.T) *client.Client {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return client.New(url, "tok")
}

// mustNotCall fails the test if the backend is contacted.
type mustNotCall struct{ t *testing.T }

func (m mustNotCall) AccessRequest(context.Context, client.AccessRequest) (*client.AccessResponse, error) {
	m.t.Errorf("the backend must not be called")
	return nil, &client.UnreachableError{Cause: errors.New("called")}
}

// panicking stands in for a bug in the call path.
type panicking struct{}

func (panicking) AccessRequest(context.Context, client.AccessRequest) (*client.AccessResponse, error) {
	panic("boom")
}

func approve(reason string) string {
	return `{"request_id":"req-1","verdict":"approve","reason":"` + reason + `","decided_at":"2026-09-14T20:00:05Z"}`
}

func deny(reason string) string {
	return `{"request_id":"req-1","verdict":"deny","reason":"` + reason + `","decided_at":null}`
}

func ptr(t time.Time) *time.Time { return &t }
