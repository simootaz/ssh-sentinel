// Package rules decides what happens to an access request before any push
// goes out, and counts admin denials for the auto-block
// (docs/architecture.md, sections 2 and 8).
//
// Order of evaluation, each rule ending the request without a push:
//
//  1. the source IP has an active row in blocked_ips: status blocked_ip, deny
//  2. the country matches a row in geo_rules: status blocked_geo, deny
//  3. an unexpired whitelist entry matches (username, context) for this
//     server or for every server: status whitelisted, approve
//  4. the request is in notify mode: status notified, approve
//
// Otherwise the request is pending and the handler pushes it. Rules run
// before the whitelist on purpose: a whitelisted user coming from a blocked
// IP or country is denied.
package rules

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Config is the auto-block tuning. Threshold denials from one IP inside
// Window insert the IP into blocked_ips for BlockDuration (0 = until an
// admin unblocks it from the app).
type Config struct {
	Threshold     int
	Window        time.Duration
	BlockDuration time.Duration
}

// DefaultConfig is 3 denials in 1 hour, blocked until unblocked.
func DefaultConfig() Config {
	return Config{Threshold: 3, Window: time.Hour, BlockDuration: 0}
}

// Engine applies the rules against the store.
type Engine struct {
	store db.Store
	clock model.Clock
	cfg   Config
}

// New builds an engine. A zero Threshold falls back to the default config.
func New(store db.Store, clock model.Clock, cfg Config) *Engine {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultConfig().Threshold
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultConfig().Window
	}
	return &Engine{store: store, clock: clock, cfg: cfg}
}

// Input is what the rules look at for one access request.
type Input struct {
	ServerID string
	Username string
	Context  string
	SourceIP *string // nil for sudo
	Country  string  // ISO 3166-1 alpha-2 from the geo lookup, empty when unknown
	Mode     string  // enforce or notify
}

// Outcome is the decision. Status is one of pending, blocked_ip, blocked_geo,
// whitelisted or notified; the matching row is attached for logging.
type Outcome struct {
	Status    string
	DecidedBy *string
	BlockedIP *model.BlockedIP
	GeoRule   *model.GeoRule
	Whitelist *model.WhitelistEntry
}

// Pending reports whether the request must be pushed and waited for.
func (o Outcome) Pending() bool { return o.Status == model.StatusPending }

func ptr(s string) *string { return &s }

// Evaluate runs the rules in order. A blocked IP match also increments the
// block's hit counter, which is what the blocked IPs screen shows.
func (e *Engine) Evaluate(ctx context.Context, in Input) (Outcome, error) {
	now := e.clock.Now()

	if in.SourceIP != nil && *in.SourceIP != "" {
		b, err := e.store.BlockedIPByIP(ctx, *in.SourceIP)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return Outcome{}, fmt.Errorf("blocked ip lookup: %w", err)
		}
		if b != nil && b.Active(now) {
			if err := e.store.RecordBlockedIPHit(ctx, b.ID, now); err != nil {
				return Outcome{}, fmt.Errorf("blocked ip hit: %w", err)
			}
			return Outcome{Status: model.StatusBlockedIP, DecidedBy: ptr(model.DecidedByAutoblock), BlockedIP: b}, nil
		}
	}

	if country := strings.ToUpper(strings.TrimSpace(in.Country)); country != "" {
		g, err := e.store.GeoRuleByCountry(ctx, country)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return Outcome{}, fmt.Errorf("geo rule lookup: %w", err)
		}
		if g != nil {
			return Outcome{Status: model.StatusBlockedGeo, DecidedBy: ptr(model.DecidedByGeoRule), GeoRule: g}, nil
		}
	}

	w, err := e.store.FindWhitelist(ctx, in.Username, in.Context, in.ServerID, now)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return Outcome{}, fmt.Errorf("whitelist lookup: %w", err)
	}
	if w != nil {
		return Outcome{Status: model.StatusWhitelisted, DecidedBy: ptr(model.DecidedByWhitelist), Whitelist: w}, nil
	}

	if in.Mode == model.ModeNotify {
		return Outcome{Status: model.StatusNotified}, nil
	}

	return Outcome{Status: model.StatusPending}, nil
}

// RecordDenial is called after an admin denied a request from ip. It counts
// the denials from that IP inside the window and, at the threshold, inserts
// (or re-arms) the blocked_ips row. It returns the block only when this
// denial created it, which is what the verdict answer reports as
// auto_blocked. Denials that happened before an unblock are not counted
// again: three new denials are needed to block the IP a second time.
func (e *Engine) RecordDenial(ctx context.Context, ip string, now time.Time) (*model.BlockedIP, error) {
	since := now.Add(-e.cfg.Window)

	existing, err := e.store.BlockedIPByIP(ctx, ip)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, fmt.Errorf("blocked ip lookup: %w", err)
	}
	if existing != nil {
		if existing.Active(now) {
			return nil, nil // already blocked, nothing new to report
		}
		if existing.ExpiresAt != nil && existing.ExpiresAt.After(since) {
			since = *existing.ExpiresAt
		}
	}

	stats, err := e.store.CountDenials(ctx, ip, since)
	if err != nil {
		return nil, fmt.Errorf("count denials: %w", err)
	}
	if stats.Count < e.cfg.Threshold {
		return nil, nil
	}

	block := &model.BlockedIP{
		IP:            ip,
		Reason:        "autoblock",
		DenialCount:   stats.Count,
		FirstDeniedAt: stats.First,
		LastDeniedAt:  stats.Last,
		CreatedAt:     now,
	}
	if e.cfg.BlockDuration > 0 {
		exp := now.Add(e.cfg.BlockDuration)
		block.ExpiresAt = &exp
	}
	created, err := e.store.UpsertBlockedIP(ctx, block)
	if err != nil {
		return nil, fmt.Errorf("insert block: %w", err)
	}
	return created, nil
}
