package rules

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

var t0 = time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)

const (
	ipA = "203.0.113.42"
	ipB = "198.51.100.7"
)

func timep(t time.Time) *time.Time { return &t }

// env is one test's store, clock and engine, with a first server enrolled.
type env struct {
	t      *testing.T
	ctx    context.Context
	store  *dbtest.Memory
	clock  *model.FakeClock
	engine *Engine
	server *model.Server // web-01
}

func newEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	e := &env{t: t, ctx: context.Background(), store: dbtest.New(), clock: model.NewFakeClock(t0)}
	e.engine = New(e.store, e.clock, cfg)
	e.server = e.addServer("web-01")
	return e
}

func (e *env) addServer(name string) *model.Server {
	e.t.Helper()
	s, err := e.store.CreateServer(e.ctx, name, "hash-"+name, model.OSLinux, e.clock.Now())
	if err != nil {
		e.t.Fatalf("CreateServer(%s): %v", name, err)
	}
	return s
}

func (e *env) addGeoRule(country string) *model.GeoRule {
	e.t.Helper()
	g, err := e.store.CreateGeoRule(e.ctx, country, nil, e.clock.Now())
	if err != nil {
		e.t.Fatalf("CreateGeoRule(%s): %v", country, err)
	}
	return g
}

func (e *env) addWhitelist(username, context string, serverID *string, expiresAt *time.Time) *model.WhitelistEntry {
	e.t.Helper()
	w, _, err := e.store.UpsertWhitelist(e.ctx, &model.WhitelistEntry{
		Username: username, Context: context, ServerID: serverID, ExpiresAt: expiresAt,
	}, e.clock.Now())
	if err != nil {
		e.t.Fatalf("UpsertWhitelist(%s, %s): %v", username, context, err)
	}
	return w
}

// addBlock inserts a blocked_ips row as the auto-block would, three denials
// a minute ago.
func (e *env) addBlock(ip string, expiresAt *time.Time) *model.BlockedIP {
	e.t.Helper()
	now := e.clock.Now()
	b, err := e.store.UpsertBlockedIP(e.ctx, &model.BlockedIP{
		IP: ip, Reason: "autoblock", DenialCount: 3,
		FirstDeniedAt: now.Add(-time.Minute), LastDeniedAt: now.Add(-time.Minute),
		CreatedAt: now, ExpiresAt: expiresAt,
	})
	if err != nil {
		e.t.Fatalf("UpsertBlockedIP(%s): %v", ip, err)
	}
	return b
}

// addDenied inserts a request from ip that an admin denied at decidedAt.
func (e *env) addDenied(ip string, decidedAt time.Time) {
	e.t.Helper()
	created := decidedAt.Add(-10 * time.Second)
	_, err := e.store.CreateRequest(e.ctx, &model.Request{
		ServerID: e.server.ID, Context: model.ContextSSH, Username: "deploy", SourceIP: ptr(ip), Hostname: "web-01",
		Status: model.StatusDenied, DecidedBy: ptr(model.DecidedByAdmin),
		CreatedAt: created, ExpiresAt: created.Add(30 * time.Second), DecidedAt: timep(decidedAt),
	})
	if err != nil {
		e.t.Fatalf("CreateRequest(denied): %v", err)
	}
}

// addTimeout inserts a request from ip that nobody answered: decided_at
// stays null, as MarkTimeout leaves it.
func (e *env) addTimeout(ip string, createdAt time.Time) {
	e.t.Helper()
	_, err := e.store.CreateRequest(e.ctx, &model.Request{
		ServerID: e.server.ID, Context: model.ContextSSH, Username: "deploy", SourceIP: ptr(ip), Hostname: "web-01",
		Status: model.StatusTimeout, DecidedBy: ptr(model.DecidedByTimeout),
		CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * time.Second),
	})
	if err != nil {
		e.t.Fatalf("CreateRequest(timeout): %v", err)
	}
}

// input is a plain ssh login of deploy from ipA on web-01, in enforce mode.
func (e *env) input() Input {
	return Input{ServerID: e.server.ID, Username: "deploy", Context: model.ContextSSH, SourceIP: ptr(ipA), Mode: model.ModeEnforce}
}

func (e *env) evaluate(in Input) Outcome {
	e.t.Helper()
	out, err := e.engine.Evaluate(e.ctx, in)
	if err != nil {
		e.t.Fatalf("Evaluate: %v", err)
	}
	return out
}

func (e *env) recordDenial(ip string) *model.BlockedIP {
	e.t.Helper()
	b, err := e.engine.RecordDenial(e.ctx, ip, e.clock.Now())
	if err != nil {
		e.t.Fatalf("RecordDenial: %v", err)
	}
	return b
}

func (e *env) blockFor(ip string) *model.BlockedIP {
	e.t.Helper()
	b, err := e.store.BlockedIPByIP(e.ctx, ip)
	if err != nil {
		e.t.Fatalf("BlockedIPByIP(%s): %v", ip, err)
	}
	return b
}

func (e *env) noBlockFor(ip string) {
	e.t.Helper()
	if _, err := e.store.BlockedIPByIP(e.ctx, ip); !errors.Is(err, db.ErrNotFound) {
		e.t.Fatalf("BlockedIPByIP(%s) = %v, want ErrNotFound", ip, err)
	}
}

func expectStatus(t *testing.T, out Outcome, status string, decidedBy *string) {
	t.Helper()
	if out.Status != status {
		t.Fatalf("status = %q, want %q", out.Status, status)
	}
	switch {
	case decidedBy == nil && out.DecidedBy != nil:
		t.Fatalf("decided_by = %q, want nil", *out.DecidedBy)
	case decidedBy != nil && out.DecidedBy == nil:
		t.Fatalf("decided_by = nil, want %q", *decidedBy)
	case decidedBy != nil && *out.DecidedBy != *decidedBy:
		t.Fatalf("decided_by = %q, want %q", *out.DecidedBy, *decidedBy)
	}
}

// Evaluate

func TestEvaluateBlockedIPWinsOverGeoRuleAndWhitelist(t *testing.T) {
	e := newEnv(t, Config{})
	block := e.addBlock(ipA, nil)
	e.addGeoRule("FR")
	e.addWhitelist("deploy", model.ContextSSH, nil, nil)

	in := e.input()
	in.Country = "FR"
	out := e.evaluate(in)

	expectStatus(t, out, model.StatusBlockedIP, ptr(model.DecidedByAutoblock))
	if out.BlockedIP == nil || out.BlockedIP.ID != block.ID {
		t.Fatalf("outcome must carry the block row, got %+v", out.BlockedIP)
	}
	if out.GeoRule != nil || out.Whitelist != nil {
		t.Errorf("only the block must be attached, got geo %+v whitelist %+v", out.GeoRule, out.Whitelist)
	}
	if out.Pending() {
		t.Error("a blocked request is not pending")
	}

	row := e.blockFor(ipA)
	if row.HitCount != 1 || row.LastHitAt == nil || !row.LastHitAt.Equal(t0) {
		t.Errorf("hit_count = %d, last_hit_at = %v; want 1 and %v", row.HitCount, row.LastHitAt, t0)
	}
}

func TestEvaluateBlockedIPCountsEveryHit(t *testing.T) {
	e := newEnv(t, Config{})
	e.addBlock(ipA, nil)

	e.evaluate(e.input())
	e.clock.Advance(5 * time.Minute)
	e.evaluate(e.input())

	row := e.blockFor(ipA)
	if row.HitCount != 2 || !row.LastHitAt.Equal(t0.Add(5*time.Minute)) {
		t.Fatalf("hit_count = %d, last_hit_at = %v; want 2 and %v", row.HitCount, row.LastHitAt, t0.Add(5*time.Minute))
	}
}

func TestEvaluateExpiredBlockIgnored(t *testing.T) {
	cases := map[string]time.Time{
		"expired a minute ago": t0.Add(-time.Minute),
		"expires right now":    t0,
	}
	for name, exp := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, Config{})
			e.addBlock(ipA, timep(exp))
			out := e.evaluate(e.input())
			expectStatus(t, out, model.StatusPending, nil)
			if row := e.blockFor(ipA); row.HitCount != 0 || row.LastHitAt != nil {
				t.Errorf("an expired block must not count hits, got %d, %v", row.HitCount, row.LastHitAt)
			}
		})
	}

	t.Run("block still active", func(t *testing.T) {
		e := newEnv(t, Config{})
		e.addBlock(ipA, timep(t0.Add(time.Minute)))
		expectStatus(t, e.evaluate(e.input()), model.StatusBlockedIP, ptr(model.DecidedByAutoblock))
	})
}

func TestEvaluateNoSourceIPSkipsIPRule(t *testing.T) {
	e := newEnv(t, Config{})
	e.addBlock(ipA, nil)

	for name, ip := range map[string]*string{"nil": nil, "empty": ptr("")} {
		t.Run(name, func(t *testing.T) {
			in := e.input()
			in.Context = model.ContextSudo
			in.SourceIP = ip
			expectStatus(t, e.evaluate(in), model.StatusPending, nil)
		})
	}
	if row := e.blockFor(ipA); row.HitCount != 0 {
		t.Errorf("hit_count = %d, want 0", row.HitCount)
	}
}

func TestEvaluateOtherIPNotBlocked(t *testing.T) {
	e := newEnv(t, Config{})
	e.addBlock(ipA, nil)
	in := e.input()
	in.SourceIP = ptr(ipB)
	expectStatus(t, e.evaluate(in), model.StatusPending, nil)
}

func TestEvaluateGeoRuleWinsOverWhitelist(t *testing.T) {
	e := newEnv(t, Config{})
	rule := e.addGeoRule("FR")
	e.addWhitelist("deploy", model.ContextSSH, nil, nil)

	in := e.input()
	in.Country = "FR"
	out := e.evaluate(in)

	expectStatus(t, out, model.StatusBlockedGeo, ptr(model.DecidedByGeoRule))
	if out.GeoRule == nil || out.GeoRule.ID != rule.ID {
		t.Fatalf("outcome must carry the geo rule, got %+v", out.GeoRule)
	}
	if out.BlockedIP != nil || out.Whitelist != nil {
		t.Errorf("only the geo rule must be attached")
	}
}

func TestEvaluateCountryCaseInsensitive(t *testing.T) {
	e := newEnv(t, Config{})
	e.addGeoRule("FR")
	for _, country := range []string{"FR", "fr", "Fr", " fr "} {
		in := e.input()
		in.Country = country
		if out := e.evaluate(in); out.Status != model.StatusBlockedGeo {
			t.Errorf("country %q: status = %q, want blocked_geo", country, out.Status)
		}
	}
	in := e.input()
	in.Country = "DE"
	expectStatus(t, e.evaluate(in), model.StatusPending, nil)
}

func TestEvaluateEmptyCountrySkipsGeoRule(t *testing.T) {
	e := newEnv(t, Config{})
	e.addGeoRule("FR")
	for _, country := range []string{"", "   "} {
		in := e.input()
		in.Country = country
		expectStatus(t, e.evaluate(in), model.StatusPending, nil)
	}
}

func TestEvaluateWhitelistWinsOverNotify(t *testing.T) {
	e := newEnv(t, Config{})
	entry := e.addWhitelist("deploy", model.ContextSSH, nil, nil)

	in := e.input()
	in.Mode = model.ModeNotify
	out := e.evaluate(in)

	expectStatus(t, out, model.StatusWhitelisted, ptr(model.DecidedByWhitelist))
	if out.Whitelist == nil || out.Whitelist.ID != entry.ID {
		t.Fatalf("outcome must carry the whitelist entry, got %+v", out.Whitelist)
	}
}

func TestEvaluateWhitelistPerServer(t *testing.T) {
	e := newEnv(t, Config{})
	other := e.addServer("db-01")
	entry := e.addWhitelist("deploy", model.ContextSSH, &e.server.ID, nil)

	out := e.evaluate(e.input())
	expectStatus(t, out, model.StatusWhitelisted, ptr(model.DecidedByWhitelist))
	if out.Whitelist.ID != entry.ID {
		t.Fatalf("matched entry %s, want %s", out.Whitelist.ID, entry.ID)
	}

	in := e.input()
	in.ServerID = other.ID
	expectStatus(t, e.evaluate(in), model.StatusPending, nil)
}

func TestEvaluateWhitelistGlobal(t *testing.T) {
	e := newEnv(t, Config{})
	other := e.addServer("db-01")
	e.addWhitelist("deploy", model.ContextSSH, nil, nil)

	for _, id := range []string{e.server.ID, other.ID} {
		in := e.input()
		in.ServerID = id
		expectStatus(t, e.evaluate(in), model.StatusWhitelisted, ptr(model.DecidedByWhitelist))
	}
}

func TestEvaluateWhitelistExpiry(t *testing.T) {
	t.Run("entry expires later", func(t *testing.T) {
		e := newEnv(t, Config{})
		e.addWhitelist("deploy", model.ContextSSH, nil, timep(t0.Add(10*time.Minute)))
		expectStatus(t, e.evaluate(e.input()), model.StatusWhitelisted, ptr(model.DecidedByWhitelist))

		e.clock.Advance(10 * time.Minute) // expires_at is exclusive
		expectStatus(t, e.evaluate(e.input()), model.StatusPending, nil)
	})

	t.Run("entry already expired", func(t *testing.T) {
		e := newEnv(t, Config{})
		e.addWhitelist("deploy", model.ContextSSH, nil, timep(t0.Add(-time.Second)))
		expectStatus(t, e.evaluate(e.input()), model.StatusPending, nil)
	})
}

func TestEvaluateWhitelistContextAndUserMustMatch(t *testing.T) {
	e := newEnv(t, Config{})
	e.addWhitelist("deploy", model.ContextSSH, nil, nil)

	sudo := e.input()
	sudo.Context = model.ContextSudo
	sudo.SourceIP = nil
	expectStatus(t, e.evaluate(sudo), model.StatusPending, nil)

	root := e.input()
	root.Username = "root"
	expectStatus(t, e.evaluate(root), model.StatusPending, nil)

	expectStatus(t, e.evaluate(e.input()), model.StatusWhitelisted, ptr(model.DecidedByWhitelist))
}

func TestEvaluateNotify(t *testing.T) {
	e := newEnv(t, Config{})
	in := e.input()
	in.Mode = model.ModeNotify
	out := e.evaluate(in)
	expectStatus(t, out, model.StatusNotified, nil)
	if out.BlockedIP != nil || out.GeoRule != nil || out.Whitelist != nil {
		t.Error("nothing matched, no row must be attached")
	}
	if out.Pending() {
		t.Error("a notified request is not pending")
	}
}

func TestEvaluatePending(t *testing.T) {
	e := newEnv(t, Config{})
	out := e.evaluate(e.input())
	expectStatus(t, out, model.StatusPending, nil)
	if !out.Pending() {
		t.Error("Pending() must be true")
	}
}

// RecordDenial

func TestRecordDenialBelowThreshold(t *testing.T) {
	e := newEnv(t, Config{})
	e.addDenied(ipA, t0.Add(-20*time.Minute))
	e.addDenied(ipA, t0.Add(-10*time.Minute))
	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil with 2 denials", b)
	}
	e.noBlockFor(ipA)
}

func TestRecordDenialAtThreshold(t *testing.T) {
	e := newEnv(t, Config{})
	e.addDenied(ipA, t0.Add(-30*time.Minute))
	e.addDenied(ipA, t0.Add(-20*time.Minute))
	e.addDenied(ipA, t0) // the denial being recorded

	b := e.recordDenial(ipA)
	if b == nil {
		t.Fatal("RecordDenial = nil, want a block at the third denial")
	}
	if b.IP != ipA || b.Reason != "autoblock" || b.DenialCount != 3 {
		t.Errorf("block = %+v, want ip %s, reason autoblock, 3 denials", b, ipA)
	}
	if !b.FirstDeniedAt.Equal(t0.Add(-30*time.Minute)) || !b.LastDeniedAt.Equal(t0) {
		t.Errorf("first/last denied = %v / %v, want %v / %v", b.FirstDeniedAt, b.LastDeniedAt, t0.Add(-30*time.Minute), t0)
	}
	if b.ExpiresAt != nil {
		t.Errorf("expires_at = %v, want nil (blocked until unblocked)", b.ExpiresAt)
	}
	if b.HitCount != 0 || b.LastHitAt != nil {
		t.Errorf("a new block starts with no hits, got %d, %v", b.HitCount, b.LastHitAt)
	}
	if !b.CreatedAt.Equal(t0) {
		t.Errorf("created_at = %v, want %v", b.CreatedAt, t0)
	}

	row := e.blockFor(ipA)
	if row.ID != b.ID || !row.Active(t0) {
		t.Errorf("stored row = %+v, want id %s and active", row, b.ID)
	}
	expectStatus(t, e.evaluate(e.input()), model.StatusBlockedIP, ptr(model.DecidedByAutoblock))
}

func TestRecordDenialBlockDuration(t *testing.T) {
	e := newEnv(t, Config{BlockDuration: 2 * time.Hour})
	for i := 0; i < 3; i++ {
		e.addDenied(ipA, t0.Add(-time.Duration(i)*time.Minute))
	}
	b := e.recordDenial(ipA)
	if b == nil || b.ExpiresAt == nil || !b.ExpiresAt.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("block = %+v, want expires_at %v", b, t0.Add(2*time.Hour))
	}
}

func TestRecordDenialIgnoresOldDenials(t *testing.T) {
	e := newEnv(t, Config{})
	e.addDenied(ipA, t0.Add(-2*time.Hour))
	e.addDenied(ipA, t0.Add(-90*time.Minute))
	e.addDenied(ipA, t0.Add(-5*time.Minute))
	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil: only one denial inside the hour", b)
	}

	e.addDenied(ipA, t0.Add(-30*time.Minute))
	e.addDenied(ipA, t0.Add(-10*time.Minute))
	b := e.recordDenial(ipA)
	if b == nil || b.DenialCount != 3 || !b.FirstDeniedAt.Equal(t0.Add(-30*time.Minute)) {
		t.Fatalf("block = %+v, want 3 denials counted from %v", b, t0.Add(-30*time.Minute))
	}
}

func TestRecordDenialIgnoresTimeoutsAndOtherIPs(t *testing.T) {
	e := newEnv(t, Config{})
	for i := 1; i <= 3; i++ {
		e.addTimeout(ipA, t0.Add(-time.Duration(i)*time.Minute))
		e.addDenied(ipB, t0.Add(-time.Duration(i)*time.Minute))
	}
	e.addDenied(ipA, t0.Add(-20*time.Minute))
	e.addDenied(ipA, t0.Add(-10*time.Minute))

	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil: timeouts and other IPs do not count", b)
	}
	e.noBlockFor(ipA)

	e.addDenied(ipA, t0)
	b := e.recordDenial(ipA)
	if b == nil || b.DenialCount != 3 {
		t.Fatalf("block = %+v, want exactly the 3 admin denials of %s", b, ipA)
	}
}

func TestRecordDenialActiveBlockUntouched(t *testing.T) {
	e := newEnv(t, Config{})
	block := e.addBlock(ipA, nil)
	e.evaluate(e.input()) // one hit on the block
	for i := 0; i < 5; i++ {
		e.addDenied(ipA, t0.Add(-time.Duration(i)*time.Minute))
	}

	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil while the IP is already blocked", b)
	}
	row := e.blockFor(ipA)
	if row.ID != block.ID || row.DenialCount != 3 || row.HitCount != 1 || row.ExpiresAt != nil {
		t.Fatalf("active block was changed: %+v", row)
	}
}

func TestRecordDenialAfterUnblock(t *testing.T) {
	e := newEnv(t, Config{})
	e.addDenied(ipA, t0.Add(-30*time.Minute))
	e.addDenied(ipA, t0.Add(-20*time.Minute))
	e.addDenied(ipA, t0.Add(-10*time.Minute))
	first := e.recordDenial(ipA)
	if first == nil {
		t.Fatal("no block after 3 denials")
	}
	e.evaluate(e.input()) // hit_count 1

	if err := e.store.UnblockIP(e.ctx, first.ID, e.clock.Now()); err != nil {
		t.Fatalf("UnblockIP: %v", err)
	}
	e.clock.Advance(time.Minute)
	expectStatus(t, e.evaluate(e.input()), model.StatusPending, nil)

	// The three denials before the unblock are inside the window but must
	// not count again.
	e.addDenied(ipA, e.clock.Now())
	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil: one denial since the unblock", b)
	}
	firstAfter := e.clock.Now()

	e.clock.Advance(time.Minute)
	e.addDenied(ipA, e.clock.Now())
	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil: two denials since the unblock", b)
	}

	e.clock.Advance(time.Minute)
	e.addDenied(ipA, e.clock.Now())
	again := e.recordDenial(ipA)
	if again == nil {
		t.Fatal("RecordDenial = nil, want the block re-armed at the third denial after the unblock")
	}
	if again.ID != first.ID {
		t.Errorf("re-armed block id = %s, want the original %s", again.ID, first.ID)
	}
	if again.DenialCount != 3 || !again.FirstDeniedAt.Equal(firstAfter) || !again.LastDeniedAt.Equal(e.clock.Now()) {
		t.Errorf("re-armed block = %+v, want 3 denials from %v to %v", again, firstAfter, e.clock.Now())
	}
	if again.HitCount != 0 || again.LastHitAt != nil || again.ExpiresAt != nil {
		t.Errorf("re-armed block must reset hits and expiry, got %+v", again)
	}
	if row := e.blockFor(ipA); !row.Active(e.clock.Now()) || row.HitCount != 0 {
		t.Errorf("stored row = %+v, want active with 0 hits", row)
	}
	expectStatus(t, e.evaluate(e.input()), model.StatusBlockedIP, ptr(model.DecidedByAutoblock))
}

func TestRecordDenialRearmsExpiredBlock(t *testing.T) {
	// A timed block that ran out counts like an unblock: only denials after
	// its expiry lead to a new block.
	e := newEnv(t, Config{BlockDuration: time.Hour})
	old := e.addBlock(ipA, timep(t0.Add(-30*time.Minute)))
	e.addDenied(ipA, t0.Add(-40*time.Minute)) // before the expiry: ignored
	e.addDenied(ipA, t0.Add(-20*time.Minute))
	e.addDenied(ipA, t0.Add(-10*time.Minute))
	if b := e.recordDenial(ipA); b != nil {
		t.Fatalf("RecordDenial = %+v, want nil: two denials since the block expired", b)
	}

	e.addDenied(ipA, t0)
	b := e.recordDenial(ipA)
	if b == nil || b.ID != old.ID || b.DenialCount != 3 {
		t.Fatalf("block = %+v, want the row %s re-armed with 3 denials", b, old.ID)
	}
	if b.ExpiresAt == nil || !b.ExpiresAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("expires_at = %v, want %v", b.ExpiresAt, t0.Add(time.Hour))
	}
}

// New

func TestNewDefaultsForZeroConfig(t *testing.T) {
	store, clock := dbtest.New(), model.NewFakeClock(t0)

	if got := New(store, clock, Config{}).cfg; got != DefaultConfig() {
		t.Errorf("zero config became %+v, want %+v", got, DefaultConfig())
	}
	if got := New(store, clock, Config{Threshold: -1, Window: -time.Minute}).cfg; got != DefaultConfig() {
		t.Errorf("negative config became %+v, want %+v", got, DefaultConfig())
	}

	custom := Config{Threshold: 5, Window: 10 * time.Minute, BlockDuration: 2 * time.Hour}
	if got := New(store, clock, custom).cfg; got != custom {
		t.Errorf("custom config became %+v, want it kept as %+v", got, custom)
	}

	partial := Config{Threshold: 2}
	want := Config{Threshold: 2, Window: time.Hour}
	if got := New(store, clock, partial).cfg; got != want {
		t.Errorf("partial config became %+v, want %+v", got, want)
	}
	if got := DefaultConfig(); got.Threshold != 3 || got.Window != time.Hour || got.BlockDuration != 0 {
		t.Errorf("DefaultConfig = %+v, want 3 denials in 1 h, blocked until unblocked", got)
	}
}
