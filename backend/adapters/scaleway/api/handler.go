// Package api is the Scaleway Serverless Functions entry point of the
// backend. Scaleway's Go runtime looks for the exported Handle function and
// calls it for every HTTP request the function receives; it forwards to the
// same http.Handler the container serves, so the two deployments run the
// same code. One function ("api") serves every route of the contract, which
// is what gives clients a single base URL on Scaleway as with the container
// (docs/architecture.md, section 8, item 11).
//
// The function does not apply migrations: the deploy runs "server migrate"
// as a step (section 3.3).
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/app"
)

// A function instance is long-lived and serves many requests, so the
// backend is built on the first call and reused afterwards. The state is
// guarded by a mutex rather than a sync.Once on purpose: a failed start
// (typically the database not answering while it cold-starts itself) is
// retried on a later call, otherwise the instance would answer 500 for as
// long as traffic keeps it alive.
var (
	mu          sync.Mutex
	backend     *app.App
	initErr     error
	lastAttempt time.Time
)

// retryAfter is the minimum delay between two failed start attempts, so
// that a burst of requests does not pile up connection timeouts.
const retryAfter = 5 * time.Second

// startTimeout bounds the database connection at start. Well below the
// function timeout (60 s) so that the caller still gets an answer.
const startTimeout = 20 * time.Second

// Scaleway captures stdout as the function logs; JSON keeps them searchable.
var log = slog.New(slog.NewJSONHandler(os.Stdout, nil))

// instance returns the backend, building it if needed.
func instance() (*app.App, error) {
	mu.Lock()
	defer mu.Unlock()

	if backend != nil {
		return backend, nil
	}
	if initErr != nil && time.Since(lastAttempt) < retryAfter {
		return nil, initErr
	}
	lastAttempt = time.Now()
	backend, initErr = start()
	return backend, initErr
}

// start reads the environment and wires the backend.
func start() (*app.App, error) {
	cfg, err := app.LoadConfig(os.Getenv)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	a, err := app.New(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	log.Info("function started", "push_enabled", a.PushEnabled)
	return a, nil
}

// Handle is what Scaleway calls. Never panics: a backend that could not
// start answers 500 with a log line on every call, and is retried.
func Handle(w http.ResponseWriter, r *http.Request) {
	a, err := instance()
	if err != nil {
		log.Error("backend not configured", "err", err, "method", r.Method, "path", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "backend not configured"})
		return
	}
	a.Handler.ServeHTTP(w, r)
}
