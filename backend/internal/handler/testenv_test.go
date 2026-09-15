package handler

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
	"github.com/simootaz/ssh-sentinel/backend/internal/rules"
)

// Shared test harness for every route group: an httptest server around the
// real handler, the in-memory store, a fake FCM client, a fake geo lookup
// and a fake clock. Each route group's tests live in their own _test.go
// file and only use what is here; helpers specific to a group are prefixed
// with the group name to avoid clashes.

// baseTime is where the fake clock starts.
var baseTime = time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)

const testAdminToken = "admin-secret-token"

type sentPush struct {
	Token string
	Data  map[string]string
}

// fakePusher records every message. Err, when set, is returned by Send.
type fakePusher struct {
	mu   sync.Mutex
	sent []sentPush
	Err  error
}

func (p *fakePusher) Send(ctx context.Context, token string, data map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Err != nil {
		return p.Err
	}
	cp := make(map[string]string, len(data))
	for k, v := range data {
		cp[k] = v
	}
	p.sent = append(p.sent, sentPush{Token: token, Data: cp})
	return nil
}

// Sent returns every message sent so far.
func (p *fakePusher) Sent() []sentPush {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]sentPush(nil), p.sent...)
}

// OfType returns the messages whose "type" is typ.
func (p *fakePusher) OfType(typ string) []sentPush {
	var out []sentPush
	for _, s := range p.Sent() {
		if s.Data["type"] == typ {
			out = append(out, s)
		}
	}
	return out
}

// Reset forgets every message sent so far.
func (p *fakePusher) Reset() {
	p.mu.Lock()
	p.sent = nil
	p.mu.Unlock()
}

// fakeGeo answers from a map. Calls counts lookups.
type fakeGeo struct {
	mu    sync.Mutex
	byIP  map[string]*model.Geo
	Calls int
}

func (g *fakeGeo) Set(ip string, geo *model.Geo) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.byIP == nil {
		g.byIP = map[string]*model.Geo{}
	}
	g.byIP[ip] = geo
}

func (g *fakeGeo) Lookup(ctx context.Context, ip string) *model.Geo {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Calls++
	return g.byIP[ip]
}

// env is one test's world.
type env struct {
	t     *testing.T
	store *dbtest.Memory
	push  *fakePusher
	geo   *fakeGeo
	clock *model.FakeClock
	srv   *httptest.Server

	// A server named web-01 enrolled at start, with its plain token.
	server      *model.Server
	serverToken string
}

// envOptions tunes the handler and rules for one test.
type envOptions struct {
	Config    Config
	Rules     rules.Config
	Dashboard fs.FS // nil: the embedded dashboard
}

// newEnv builds the harness with default timing and rules.
func newEnv(t *testing.T) *env { return newEnvWith(t, envOptions{}) }

// newEnvWith builds the harness with custom timing or rules; zero values keep the defaults.
func newEnvWith(t *testing.T, o envOptions) *env {
	t.Helper()
	e := &env{
		t:     t,
		store: dbtest.New(),
		push:  &fakePusher{},
		geo:   &fakeGeo{},
		clock: model.NewFakeClock(baseTime),
	}
	rcfg := o.Rules
	if rcfg.Threshold == 0 {
		rcfg = rules.DefaultConfig()
	}
	h := New(Deps{
		Store:     e.store,
		Push:      e.push,
		Geo:       e.geo,
		Clock:     e.clock,
		Auth:      auth.New(e.store, testAdminToken),
		Rules:     rules.New(e.store, e.clock, rcfg),
		Config:    o.Config,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Dashboard: o.Dashboard,
	})
	e.srv = httptest.NewServer(h)
	t.Cleanup(e.srv.Close)
	e.server, e.serverToken = e.enrollServer("web-01", model.OSLinux)
	return e
}
