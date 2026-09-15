package handler

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// whitelistAdd inserts one whitelist row straight into the store. Context
// defaults to ssh; ServerID nil is a global entry; ExpiresAt nil is permanent.
func whitelistAdd(e *env, entry model.WhitelistEntry) *model.WhitelistEntry {
	e.t.Helper()
	if entry.Context == "" {
		entry.Context = model.ContextSSH
	}
	out, _, err := e.store.UpsertWhitelist(context.Background(), &entry, e.clock.Now())
	if err != nil {
		e.t.Fatalf("add whitelist entry: %v", err)
	}
	return out
}

// whitelistServers returns the server field of every item, "global" for null, sorted.
func whitelistServers(items []model.WhitelistItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Server == nil {
			out = append(out, "global")
			continue
		}
		out = append(out, *it.Server)
	}
	sort.Strings(out)
	return out
}

// whitelistContractKeys are the 8 keys of a whitelist item, in sorted order.
var whitelistContractKeys = []string{
	"context", "created_at", "created_by_device", "created_from_request",
	"expires_at", "id", "server", "username",
}

func whitelistThreeEntries(e *env) (global, web, dbEntry *model.WhitelistEntry) {
	e.t.Helper()
	dbSrv, _ := e.enrollServer("db-01", model.OSLinux)
	global = whitelistAdd(e, model.WhitelistEntry{Username: "ansible"})
	web = whitelistAdd(e, model.WhitelistEntry{Username: "deploy", ServerID: &e.server.ID})
	dbEntry = whitelistAdd(e, model.WhitelistEntry{Username: "dba", ServerID: &dbSrv.ID})
	return global, web, dbEntry
}

func TestWhitelistListAdminSeesAll(t *testing.T) {
	e := newEnv(t)
	whitelistThreeEntries(e)

	st, body := e.admin(http.MethodGet, "/whitelist", nil)
	mustStatus(t, st, http.StatusOK, body)
	resp := decode[model.ListResponse[model.WhitelistItem]](t, body)
	got := whitelistServers(resp.Items)
	if strings.Join(got, ",") != "db-01,global,web-01" {
		t.Fatalf("admin sees %v, want db-01, global and web-01", got)
	}
}

func TestWhitelistListAgentSeesOwnAndGlobal(t *testing.T) {
	e := newEnv(t)
	whitelistThreeEntries(e)

	st, body := e.agent(http.MethodGet, "/whitelist", nil)
	mustStatus(t, st, http.StatusOK, body)
	resp := decode[model.ListResponse[model.WhitelistItem]](t, body)
	got := whitelistServers(resp.Items)
	if strings.Join(got, ",") != "global,web-01" {
		t.Fatalf("agent of web-01 sees %v, want global and web-01 only", got)
	}
}

func TestWhitelistListEmpty(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodGet, "/whitelist", nil)
	mustStatus(t, st, http.StatusOK, body)
	if !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("items must be an empty array, body: %s", body)
	}
}

func TestWhitelistListDropsExpired(t *testing.T) {
	e := newEnv(t)
	past := baseTime.Add(-time.Hour)
	future := baseTime.Add(time.Hour)
	expired := whitelistAdd(e, model.WhitelistEntry{Username: "gone", ExpiresAt: &past})
	alive := whitelistAdd(e, model.WhitelistEntry{Username: "here", ExpiresAt: &future})

	st, body := e.admin(http.MethodGet, "/whitelist", nil)
	mustStatus(t, st, http.StatusOK, body)
	resp := decode[model.ListResponse[model.WhitelistItem]](t, body)
	if len(resp.Items) != 1 || resp.Items[0].ID != alive.ID {
		t.Fatalf("items %+v, want only the unexpired entry", resp.Items)
	}
	// Lazy deletion: the expired row is gone from the store after the GET.
	if _, err := e.store.WhitelistByID(context.Background(), expired.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expired entry still in the store, err %v", err)
	}
}

func TestWhitelistItemShape(t *testing.T) {
	e := newEnv(t)
	dev := e.addDevice("Pixel 8")
	req, err := e.store.CreateRequest(context.Background(), &model.Request{
		ServerID: e.server.ID, Context: model.ContextSSH, Username: "deploy", Hostname: "web-01",
		Status: model.StatusApproved, CreatedAt: baseTime, ExpiresAt: baseTime.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	exp := baseTime.Add(time.Hour)
	full := whitelistAdd(e, model.WhitelistEntry{
		Username: "deploy", ServerID: &e.server.ID, ExpiresAt: &exp,
		CreatedFrom: &req.ID, CreatedByDevice: &dev.ID,
	})
	bare := whitelistAdd(e, model.WhitelistEntry{Username: "ansible"})

	st, body := e.admin(http.MethodGet, "/whitelist", nil)
	mustStatus(t, st, http.StatusOK, body)
	byID := map[string]map[string]any{}
	for _, raw := range decodeMap(t, body)["items"].([]any) {
		item := raw.(map[string]any)
		byID[item["id"].(string)] = item
	}
	if len(byID) != 2 {
		t.Fatalf("got %d items, want 2; body: %s", len(byID), body)
	}

	for id, item := range byID {
		keys := make([]string, 0, len(item))
		for k := range item {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if strings.Join(keys, ",") != strings.Join(whitelistContractKeys, ",") {
			t.Fatalf("item %s keys %v, want %v", id, keys, whitelistContractKeys)
		}
	}

	f := byID[full.ID]
	if f["server"] != "web-01" || f["expires_at"] != exp.Format(time.RFC3339) {
		t.Fatalf("full entry server %v expires_at %v", f["server"], f["expires_at"])
	}
	if f["created_from_request"] != req.ID || f["created_by_device"] != "Pixel 8" {
		t.Fatalf("full entry created_from_request %v created_by_device %v", f["created_from_request"], f["created_by_device"])
	}

	b := byID[bare.ID]
	for _, k := range []string{"server", "expires_at", "created_from_request", "created_by_device"} {
		if v, ok := b[k]; !ok || v != nil {
			t.Fatalf("bare entry %s must be present and null, got %v", k, v)
		}
	}
}

func TestWhitelistCreateDefaults(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/whitelist", map[string]any{"username": "ansible"})
	mustStatus(t, st, http.StatusCreated, body)
	m := decodeMap(t, body)
	if m["username"] != "ansible" || m["context"] != model.ContextSSH {
		t.Fatalf("username %v context %v", m["username"], m["context"])
	}
	for _, k := range []string{"server", "expires_at", "created_from_request", "created_by_device"} {
		if v, ok := m[k]; !ok || v != nil {
			t.Fatalf("%s must be present and null, got %v", k, v)
		}
	}
	if !isUUID(m["id"].(string)) {
		t.Fatalf("id %v", m["id"])
	}
	if m["created_at"] != baseTime.Format(time.RFC3339) {
		t.Fatalf("created_at %v, want %s", m["created_at"], baseTime.Format(time.RFC3339))
	}
}

func TestWhitelistCreateWithServerAndTTL(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/whitelist", map[string]any{
		"username": "deploy", "context": "sudo", "server": "web-01", "ttl_seconds": 86400,
	})
	mustStatus(t, st, http.StatusCreated, body)
	item := decode[model.WhitelistItem](t, body)
	if item.Server == nil || *item.Server != "web-01" || item.Context != model.ContextSudo {
		t.Fatalf("item %+v", item)
	}
	if item.ExpiresAt == nil || !item.ExpiresAt.Equal(baseTime.Add(24*time.Hour)) {
		t.Fatalf("expires_at %v, want now + 24 h", item.ExpiresAt)
	}
	// The entry is what the rules will find for that server.
	found, err := e.store.FindWhitelist(context.Background(), "deploy", model.ContextSudo, e.server.ID, e.clock.Now())
	if err != nil || found.ID != item.ID {
		t.Fatalf("FindWhitelist: %+v, %v", found, err)
	}
}

func TestWhitelistCreateRefreshesExisting(t *testing.T) {
	e := newEnv(t)
	first := map[string]any{"username": "deploy", "context": "ssh", "server": "web-01", "ttl_seconds": 3600}
	st, body := e.admin(http.MethodPost, "/whitelist", first)
	mustStatus(t, st, http.StatusCreated, body)
	created := decode[model.WhitelistItem](t, body)

	e.clock.Advance(10 * time.Minute)
	second := map[string]any{"username": "deploy", "context": "ssh", "server": "web-01", "ttl_seconds": 7200}
	st, body = e.admin(http.MethodPost, "/whitelist", second)
	mustStatus(t, st, http.StatusOK, body)
	refreshed := decode[model.WhitelistItem](t, body)

	if refreshed.ID != created.ID {
		t.Fatalf("refresh changed the id: %s -> %s", created.ID, refreshed.ID)
	}
	want := e.clock.Now().Add(2 * time.Hour)
	if refreshed.ExpiresAt == nil || !refreshed.ExpiresAt.Equal(want) {
		t.Fatalf("expires_at %v, want %v", refreshed.ExpiresAt, want)
	}

	// Same user, other context: a separate entry, not a refresh.
	st, body = e.admin(http.MethodPost, "/whitelist", map[string]any{"username": "deploy", "context": "sudo", "server": "web-01"})
	mustStatus(t, st, http.StatusCreated, body)
}

func TestWhitelistCreateUnknownServer(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/whitelist", map[string]any{"username": "deploy", "server": "nope-99"})
	mustStatus(t, st, http.StatusNotFound, body)
	if decode[model.ErrorResponse](t, body).Error != "unknown server" {
		t.Fatalf("body %s", body)
	}
}

func TestWhitelistCreateBadBody(t *testing.T) {
	e := newEnv(t)
	cases := map[string]any{
		"empty username":      map[string]any{"username": ""},
		"blank username":      map[string]any{"username": "   "},
		"missing username":    map[string]any{"context": "ssh"},
		"bad context":         map[string]any{"username": "x", "context": "root"},
		"ttl zero":            map[string]any{"username": "x", "ttl_seconds": 0},
		"ttl negative":        map[string]any{"username": "x", "ttl_seconds": -5},
		"ttl beyond century":  map[string]any{"username": "x", "ttl_seconds": 4000000000},
		"ttl not a number":    map[string]any{"username": "x", "ttl_seconds": "1h"},
		"invalid JSON":        []byte("{not json"),
		"array instead of {}": []byte("[]"),
	}
	for name, body := range cases {
		st, out := e.admin(http.MethodPost, "/whitelist", body)
		if st != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400; body: %s", name, st, out)
			continue
		}
		if decode[model.ErrorResponse](t, out).Error == "" {
			t.Errorf("%s: error body missing: %s", name, out)
		}
	}
	// Nothing was written by any of them.
	st, out := e.admin(http.MethodGet, "/whitelist", nil)
	mustStatus(t, st, http.StatusOK, out)
	if n := len(decode[model.ListResponse[model.WhitelistItem]](t, out).Items); n != 0 {
		t.Fatalf("%d entries created by rejected bodies", n)
	}
}

func TestWhitelistCreateServerTokenForbidden(t *testing.T) {
	e := newEnv(t)
	st, body := e.agent(http.MethodPost, "/whitelist", map[string]any{"username": "deploy"})
	mustStatus(t, st, http.StatusForbidden, body)
}

func TestWhitelistDelete(t *testing.T) {
	e := newEnv(t)
	entry := whitelistAdd(e, model.WhitelistEntry{Username: "deploy", ServerID: &e.server.ID})

	st, body := e.admin(http.MethodDelete, "/whitelist/"+entry.ID, nil)
	mustStatus(t, st, http.StatusNoContent, body)
	if len(body) != 0 {
		t.Fatalf("204 must have no body, got %s", body)
	}

	st, body = e.admin(http.MethodDelete, "/whitelist/"+entry.ID, nil)
	mustStatus(t, st, http.StatusNotFound, body)
	if decode[model.ErrorResponse](t, body).Error != "not found" {
		t.Fatalf("body %s", body)
	}

	_, err := e.store.FindWhitelist(context.Background(), "deploy", model.ContextSSH, e.server.ID, e.clock.Now())
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("entry still found after delete, err %v", err)
	}
}

func TestWhitelistDeleteMalformedID(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"not-a-uuid", "12345", "9a7d3e10-6b2f-4c55-8e0a"} {
		st, body := e.admin(http.MethodDelete, "/whitelist/"+id, nil)
		if st != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404; body: %s", id, st, body)
		}
	}
}

func TestWhitelistDeleteServerTokenForbidden(t *testing.T) {
	e := newEnv(t)
	entry := whitelistAdd(e, model.WhitelistEntry{Username: "deploy"})
	st, body := e.agent(http.MethodDelete, "/whitelist/"+entry.ID, nil)
	mustStatus(t, st, http.StatusForbidden, body)
	if _, err := e.store.WhitelistByID(context.Background(), entry.ID); err != nil {
		t.Fatalf("entry must survive a forbidden delete, err %v", err)
	}
}
