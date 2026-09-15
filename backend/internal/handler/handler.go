// Package handler builds the one http.Handler that serves CONTRACT v1 and
// the v1.1 additions (docs/architecture.md, section 5), the dashboard
// included. It knows nothing about the provider: cmd/server and
// adapters/scaleway/api both call New and serve the result.
package handler

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
	"github.com/simootaz/ssh-sentinel/backend/internal/rules"
)

// Pusher sends one FCM data message. internal/fcm implements it; tests use a fake.
type Pusher interface {
	Send(ctx context.Context, token string, data map[string]string) error
}

// GeoLookup resolves a source IP. It returns nil when unknown or disabled and
// must never take longer than about a second.
type GeoLookup interface {
	Lookup(ctx context.Context, ip string) *model.Geo
}

// Config is the timing of the access-request route.
type Config struct {
	VerdictWait  time.Duration // how long POST /access-request waits for a verdict (25 s)
	PollInterval time.Duration // how often it re-reads the requests row (1 s)
	RequestTTL   time.Duration // expires_at = created_at + RequestTTL (30 s)
	PushTimeout  time.Duration // budget for one round of pushes to every phone
}

// DefaultConfig is the timing budget of the architecture: agent 30 s,
// backend wait 25 s, request expiry 30 s.
func DefaultConfig() Config {
	return Config{VerdictWait: 25 * time.Second, PollInterval: time.Second, RequestTTL: 30 * time.Second, PushTimeout: 5 * time.Second}
}

// Deps is everything the routes need.
type Deps struct {
	Store  db.Store
	Push   Pusher
	Geo    GeoLookup
	Clock  model.Clock
	Auth   *auth.Authenticator
	Rules  *rules.Engine
	Config Config
	Log    *slog.Logger

	// Dashboard is the file tree served at /dashboard. Nil means the
	// dashboard embedded in the web module; tests may pass their own.
	Dashboard fs.FS
}

// Handler holds the dependencies; one method per route.
type Handler struct {
	store db.Store
	push  Pusher
	geo   GeoLookup
	clock model.Clock
	auth  *auth.Authenticator
	rules *rules.Engine
	cfg   Config
	log   *slog.Logger

	dashboard map[string]staticFile // path inside the web folder -> file
}

// New wires every route of the contract on a Go 1.22 ServeMux. The
// dashboard files are read from the embedded tree here, once.
func New(d Deps) http.Handler {
	h := &Handler{store: d.Store, push: d.Push, geo: d.Geo, clock: d.Clock, auth: d.Auth, rules: d.Rules, cfg: d.Config, log: d.Log}
	if h.clock == nil {
		h.clock = model.SystemClock{}
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	def := DefaultConfig()
	if h.cfg.VerdictWait <= 0 {
		h.cfg.VerdictWait = def.VerdictWait
	}
	if h.cfg.PollInterval <= 0 {
		h.cfg.PollInterval = def.PollInterval
	}
	if h.cfg.RequestTTL <= 0 {
		h.cfg.RequestTTL = def.RequestTTL
	}
	if h.cfg.PushTimeout <= 0 {
		h.cfg.PushTimeout = def.PushTimeout
	}

	// An embedded tree cannot fail to read; a tree passed by a test can, and
	// a dashboard that is not there is a 404 on every file, not a crash.
	files, err := loadDashboard(d.Dashboard)
	if err != nil {
		h.log.Error("dashboard: cannot read the embedded files, /dashboard answers 404", "err", err)
		files = map[string]staticFile{}
	}
	h.dashboard = files

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	h.registerDashboard(mux)

	mux.HandleFunc("POST /access-request", h.requireServer(h.accessRequest))
	mux.HandleFunc("POST /verdict", h.requireAdmin(h.verdict))
	mux.HandleFunc("GET /history", h.requireAdmin(h.history))

	mux.HandleFunc("GET /whitelist", h.requireAny(h.listWhitelist))
	mux.HandleFunc("POST /whitelist", h.requireAdmin(h.createWhitelist))
	mux.HandleFunc("DELETE /whitelist/{id}", h.requireAdmin(h.deleteWhitelist))

	mux.HandleFunc("POST /devices", h.requireAdmin(h.createDevice))
	mux.HandleFunc("GET /devices", h.requireAdmin(h.listDevices))
	mux.HandleFunc("DELETE /devices/{id}", h.requireAdmin(h.deleteDevice))

	mux.HandleFunc("GET /blocked-ips", h.requireAdmin(h.listBlockedIPs))
	mux.HandleFunc("DELETE /blocked-ips/{id}", h.requireAdmin(h.deleteBlockedIP))

	mux.HandleFunc("GET /geo-rules", h.requireAdmin(h.listGeoRules))
	mux.HandleFunc("POST /geo-rules", h.requireAdmin(h.createGeoRule))
	mux.HandleFunc("DELETE /geo-rules/{id}", h.requireAdmin(h.deleteGeoRule))

	return h.outer(mux)
}

// outer wraps the mux: JSON bodies for the mux's own 404/405, a JSON 500 on
// panic, and one log line per call.
func (h *Handler) outer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		jw := &jsonErrorWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				h.log.Error("panic", "method", r.Method, "path", r.URL.Path, "panic", rec)
				if !jw.wrote {
					writeError(jw, http.StatusInternalServerError, "internal error")
				}
			}
			h.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", jw.status, "ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(jw, r)
	})
}

// authenticate runs the authenticator and answers 401 itself on failure.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (*auth.Principal, bool) {
	p, err := h.auth.Authenticate(r.Context(), r)
	switch {
	case errors.Is(err, auth.ErrNoToken), errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return nil, false
	case err != nil:
		h.log.Error("auth", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return nil, false
	}
	if p.Kind == auth.KindServer {
		// last_seen_at is informational: a failure to update it never blocks the call.
		if err := h.store.TouchServer(r.Context(), p.Server.ID, h.clock.Now()); err != nil {
			h.log.Warn("touch server", "server", p.Server.Name, "err", err)
		}
	}
	return p, true
}

// requireAdmin accepts the admin token only (the phones).
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		if p.Kind != auth.KindAdmin {
			writeError(w, http.StatusForbidden, "admin token required")
			return
		}
		next(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	}
}

// requireServer accepts a server token only (the agents).
func (h *Handler) requireServer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		if p.Kind != auth.KindServer {
			writeError(w, http.StatusForbidden, "server token required")
			return
		}
		next(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	}
}

// requireAny accepts both: GET /whitelist serves the app and the agents.
func (h *Handler) requireAny(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		next(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	}
}
