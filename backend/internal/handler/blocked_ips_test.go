package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

var blockedIPsContractKeys = []string{
	"id", "ip", "reason", "denial_count", "first_denied_at", "last_denied_at",
	"hit_count", "last_hit_at", "created_at", "expires_at",
}

// blockedIPsSeed inserts a block for ip the way the auto-block does: three
// denials over the last twenty minutes, created now. expiresAt nil means
// until unblocked.
func blockedIPsSeed(e *env, ip string, expiresAt *time.Time) *model.BlockedIP {
	e.t.Helper()
	now := e.clock.Now()
	b, err := e.store.UpsertBlockedIP(context.Background(), &model.BlockedIP{
		IP:            ip,
		Reason:        "autoblock",
		DenialCount:   3,
		FirstDeniedAt: now.Add(-20 * time.Minute),
		LastDeniedAt:  now,
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		e.t.Fatalf("seed blocked ip %s: %v", ip, err)
	}
	return b
}

func blockedIPsList(e *env) []model.BlockedIPItem {
	e.t.Helper()
	st, body := e.admin(http.MethodGet, "/blocked-ips", nil)
	mustStatus(e.t, st, http.StatusOK, body)
	return decode[model.ListResponse[model.BlockedIPItem]](e.t, body).Items
}

func TestBlockedIPsListShape(t *testing.T) {
	e := newEnv(t)
	seeded := blockedIPsSeed(e, "203.0.113.42", nil)

	st, body := e.admin(http.MethodGet, "/blocked-ips", nil)
	mustStatus(t, st, http.StatusOK, body)
	raw, ok := decodeMap(t, body)["items"].([]any)
	if !ok || len(raw) != 1 {
		t.Fatalf("want one item, got %s", body)
	}
	m := raw[0].(map[string]any)
	devicesMustHaveExactKeys(t, m, blockedIPsContractKeys...)
	if m["expires_at"] != nil || m["last_hit_at"] != nil {
		t.Fatalf("expires_at and last_hit_at must be null: %s", body)
	}
	if m["hit_count"] != float64(0) || m["denial_count"] != float64(3) {
		t.Fatalf("counts: %s", body)
	}

	item := blockedIPsList(e)[0]
	if item.ID != seeded.ID || item.IP != "203.0.113.42" || item.Reason != "autoblock" {
		t.Fatalf("item %+v", item)
	}
	if !item.CreatedAt.Equal(baseTime) || !item.LastDeniedAt.Equal(baseTime) || !item.FirstDeniedAt.Equal(baseTime.Add(-20*time.Minute)) {
		t.Fatalf("times %+v", item)
	}
}

func TestBlockedIPsListSkipsExpired(t *testing.T) {
	e := newEnv(t)
	past := baseTime.Add(-time.Hour)
	future := baseTime.Add(time.Hour)
	blockedIPsSeed(e, "198.51.100.7", &past)             // unblocked an hour ago
	active := blockedIPsSeed(e, "203.0.113.42", &future) // temporary block still running

	items := blockedIPsList(e)
	if len(items) != 1 || items[0].ID != active.ID {
		t.Fatalf("want only the active block, got %+v", items)
	}
	if items[0].ExpiresAt == nil || !items[0].ExpiresAt.Equal(future) {
		t.Fatalf("expires_at %v, want %v", items[0].ExpiresAt, future)
	}

	// Once the clock reaches expires_at the temporary block is gone too.
	e.clock.Set(future)
	if items := blockedIPsList(e); len(items) != 0 {
		t.Fatalf("want no block after expiry, got %+v", items)
	}
}

func TestBlockedIPsListEmptyIsNotNull(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodGet, "/blocked-ips", nil)
	mustStatus(t, st, http.StatusOK, body)
	items, ok := decodeMap(t, body)["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("want items: [], got %s", body)
	}
}

func TestBlockedIPsDelete(t *testing.T) {
	e := newEnv(t)
	b := blockedIPsSeed(e, "203.0.113.42", nil)
	e.clock.Advance(10 * time.Minute)

	st, body := e.admin(http.MethodDelete, "/blocked-ips/"+b.ID, nil)
	mustStatus(t, st, http.StatusNoContent, body)
	if len(body) != 0 {
		t.Fatalf("204 with a body: %s", body)
	}
	if items := blockedIPsList(e); len(items) != 0 {
		t.Fatalf("still listed after unblock: %+v", items)
	}

	// The row stays, expired at the unblock time, so that the denials before
	// the unblock are not counted again by the auto-block.
	row, err := e.store.BlockedIPByIP(context.Background(), "203.0.113.42")
	if err != nil {
		t.Fatalf("row gone after unblock: %v", err)
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.Equal(baseTime.Add(10*time.Minute)) {
		t.Fatalf("expires_at %v, want the unblock time", row.ExpiresAt)
	}

	st, body = e.admin(http.MethodDelete, "/blocked-ips/"+b.ID, nil)
	mustStatus(t, st, http.StatusNotFound, body)
}

func TestBlockedIPsDeleteUnknownIs404(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{dbtest.NewID(), "not-a-uuid", "203.0.113.42"} {
		st, body := e.admin(http.MethodDelete, "/blocked-ips/"+id, nil)
		mustStatus(t, st, http.StatusNotFound, body)
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("body %s", body)
		}
	}
}

func TestBlockedIPsServerTokenIs403(t *testing.T) {
	e := newEnv(t)
	b := blockedIPsSeed(e, "203.0.113.42", nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/blocked-ips"},
		{http.MethodDelete, "/blocked-ips/" + b.ID},
	} {
		st, body := e.agent(c.method, c.path, nil)
		mustStatus(t, st, http.StatusForbidden, body)
	}
	if items := blockedIPsList(e); len(items) != 1 {
		t.Fatalf("block changed by a forbidden call: %+v", items)
	}
}
