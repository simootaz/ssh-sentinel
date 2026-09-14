package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/app"
)

// Timeouts of the HTTP server.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
	startupTimeout    = 30 * time.Second
	drainTimeout      = 30 * time.Second
)

// runServe is the container entrypoint: configuration, database, migrations,
// then the HTTP server until SIGINT or SIGTERM, then a graceful drain.
func runServe(getenv func(string) string, stdout io.Writer) int {
	// One JSON line per event on stdout: that is what Docker, compose and
	// every log collector pick up without configuration.
	log := slog.New(slog.NewJSONHandler(stdout, nil))

	cfg, err := app.LoadConfig(getenv)
	if err != nil {
		log.Error("startup", "err", err)
		return 1
	}

	// ctx ends on Ctrl+C or on the stop signal Docker sends. Everything
	// below that waits on ctx is cut short by it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startCtx, cancelStart := context.WithTimeout(ctx, startupTimeout)
	a, err := app.New(startCtx, cfg, log)
	if err != nil {
		cancelStart()
		log.Error("startup", "err", err)
		return 1
	}
	defer a.Close()

	// Migrate on start: the container is the only writer of the schema in
	// the compose deployment, and a second replica applying the same files
	// finds them already recorded.
	if err := a.DB.Migrate(startCtx); err != nil {
		cancelStart()
		log.Error("startup: migrate", "err", err)
		return 1
	}
	cancelStart()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           a.Handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		// No write timeout: POST /access-request holds the connection open
		// for up to VERDICT_WAIT_SECONDS (25 s) while it waits for the phone,
		// and a write timeout would cut that answer off. The reverse proxy in
		// front of the container is where a global limit belongs.
		WriteTimeout: 0,
		IdleTimeout:  idleTimeout,
		// Server-side errors (bad TLS handshakes, aborted connections) go
		// through the same JSON logger instead of the plain stderr default.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.ListenAddr, "push_enabled", a.PushEnabled, "version", version)

	select {
	case err := <-done:
		// ListenAndServe only returns on its own when it could not listen
		// (port in use, bad address); ErrServerClosed comes from Shutdown.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			return 1
		}
		return 0
	case <-ctx.Done():
	}

	// Stop accepting connections and let the in-flight ones finish; the
	// longest is an access-request waiting its 25 s, so 30 s is enough.
	log.Info("shutting down", "drain_timeout", drainTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
		return 1
	}
	log.Info("stopped")
	return 0
}
