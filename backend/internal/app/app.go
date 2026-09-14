package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/fcm"
	"github.com/simootaz/ssh-sentinel/backend/internal/geo"
	"github.com/simootaz/ssh-sentinel/backend/internal/handler"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
	"github.com/simootaz/ssh-sentinel/backend/internal/rules"
)

// App is a wired backend: the handler to serve, the database to migrate and
// close, and what the entry points report at start.
type App struct {
	Handler     http.Handler
	DB          *db.Postgres
	Log         *slog.Logger
	PushEnabled bool
}

// New opens the database and builds the handler with every dependency of
// the contract. It does not apply the migrations: the container does that
// on start (cmd/server serve) and the Scaleway deploy runs "server migrate"
// as a step (docs/architecture.md, section 3.3), so the function itself
// never alters the schema.
func New(ctx context.Context, cfg Config, log *slog.Logger) (*App, error) {
	if log == nil {
		log = slog.Default()
	}

	pg, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	var push handler.Pusher
	if cfg.PushEnabled() {
		client, err := fcm.New(cfg.FCMProjectID, []byte(cfg.FCMServiceAccountJSON), nil)
		if err != nil {
			pg.Close()
			return nil, fmt.Errorf("fcm client: %w", err)
		}
		push = client
	} else {
		log.Warn("push disabled: no FCM configuration, requests are stored but no phone is notified")
		push = noPush{}
	}

	if cfg.GeoLookupURL == "" {
		log.Info("geo lookup disabled: GEO_LOOKUP_URL is empty, geo rules never match")
	}

	// The rules and the handler share one clock. Production uses the system
	// clock; the handler tests inject a fake one to run the 25 s wait in no time.
	clock := model.SystemClock{}

	engine := rules.New(pg, clock, rules.Config{
		Threshold:     cfg.AutoblockThreshold,
		Window:        cfg.AutoblockWindow,
		BlockDuration: cfg.AutoblockDuration,
	})

	// Only the verdict wait is configurable. The poll interval, the request
	// expiry (the contract's created_at + 30 s) and the push budget keep the
	// defaults of the handler package.
	hcfg := handler.DefaultConfig()
	hcfg.VerdictWait = cfg.VerdictWait

	h := handler.New(handler.Deps{
		Store:  pg,
		Push:   push,
		Geo:    geo.New(cfg.GeoLookupURL, nil),
		Clock:  clock,
		Auth:   auth.New(pg, cfg.AdminToken),
		Rules:  engine,
		Config: hcfg,
		Log:    log,
	})

	return &App{Handler: h, DB: pg, Log: log, PushEnabled: cfg.PushEnabled()}, nil
}

// Close releases the database pool. Call it once the HTTP server has drained.
func (a *App) Close() error {
	if a.DB == nil {
		return nil
	}
	return a.DB.Close()
}

// noPush is the Pusher used when FCM is not configured. It accepts every
// message and delivers nothing; the one warning logged by New is the only
// trace. Local smoke tests simulate the phone with curl instead (TESTING.md).
type noPush struct{}

func (noPush) Send(context.Context, string, map[string]string) error { return nil }
