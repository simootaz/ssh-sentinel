package handler

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// historyAdd inserts one request row on srv straight into the store and
// returns it. Zero fields get sensible defaults: ssh context, hostname =
// server name, created now, expires 30 s later, status pending.
func historyAdd(e *env, srv *model.Server, r model.Request) *model.Request {
	e.t.Helper()
	r.ServerID = srv.ID
	if r.Hostname == "" {
		r.Hostname = srv.Name
	}
	if r.Context == "" {
		r.Context = model.ContextSSH
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = e.clock.Now()
	}
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = r.CreatedAt.Add(30 * time.Second)
	}
	if r.Status == "" {
		r.Status = model.StatusPending
	}
	out, err := e.store.CreateRequest(context.Background(), &r)
	if err != nil {
		e.t.Fatalf("create request: %v", err)
	}
	return out
}

// historyGet calls GET /history with the admin token and decodes the answer.
func historyGet(e *env, query string) model.HistoryResponse {
	e.t.Helper()
	st, body := e.admin(http.MethodGet, "/history"+query, nil)
	mustStatus(e.t, st, http.StatusOK, body)
	return decode[model.HistoryResponse](e.t, body)
}

// historyKeys returns the sorted keys of a JSON object.
func historyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// historyContractKeys are the 15 keys of a history item, in sorted order.
var historyContractKeys = []string{
	"command", "context", "created_at", "decided_at", "decided_by", "decided_by_device",
	"expires_at", "geo", "hostname", "id", "server", "source_ip", "status", "tty", "username",
}

func TestHistoryEmpty(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodGet, "/history", nil)
	mustStatus(t, st, http.StatusOK, body)
	if !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("items must be an empty array, body: %s", body)
	}
	if _, ok := decodeMap(t, body)["next_before"]; ok {
		t.Fatalf("next_before must be absent on the last page, body: %s", body)
	}
}

func TestHistoryItemShapeSudo(t *testing.T) {
	e := newEnv(t)
	dev := e.addDevice("Pixel 8")
	decidedAt := baseTime.Add(27 * time.Second)
	historyAdd(e, e.server, model.Request{
		Context:         model.ContextSudo,
		Username:        "deploy",
		TTY:             strp("pts/0"),
		Command:         strp("sudo systemctl restart nginx"),
		Status:          model.StatusApproved,
		DecidedBy:       strp(model.DecidedByAdmin),
		DecidedByDevice: &dev.ID,
		DecidedAt:       &decidedAt,
	})

	st, body := e.admin(http.MethodGet, "/history", nil)
	mustStatus(t, st, http.StatusOK, body)
	items := decodeMap(t, body)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1; body: %s", len(items), body)
	}
	item := items[0].(map[string]any)

	got := historyKeys(item)
	if strings.Join(got, ",") != strings.Join(historyContractKeys, ",") {
		t.Fatalf("item keys %v, want %v", got, historyContractKeys)
	}
	for _, k := range []string{"source_ip", "geo"} {
		if v, ok := item[k]; !ok || v != nil {
			t.Fatalf("%s must be present and null, got %v", k, v)
		}
	}
	if item["server"] != "web-01" || item["hostname"] != "web-01" {
		t.Fatalf("server %v hostname %v, want web-01", item["server"], item["hostname"])
	}
	if item["decided_by_device"] != "Pixel 8" {
		t.Fatalf("decided_by_device %v, want the device label", item["decided_by_device"])
	}
	if item["decided_by"] != model.DecidedByAdmin || item["status"] != model.StatusApproved {
		t.Fatalf("decided_by %v status %v", item["decided_by"], item["status"])
	}
	if item["command"] != "sudo systemctl restart nginx" || item["tty"] != "pts/0" {
		t.Fatalf("command %v tty %v", item["command"], item["tty"])
	}
	if item["decided_at"] != decidedAt.Format(time.RFC3339) {
		t.Fatalf("decided_at %v, want %s", item["decided_at"], decidedAt.Format(time.RFC3339))
	}
}

func TestHistoryItemGeo(t *testing.T) {
	e := newEnv(t)
	historyAdd(e, e.server, model.Request{
		Username: "root",
		SourceIP: strp("203.0.113.42"),
		Geo:      &model.Geo{Country: "FR", City: "Paris", ASN: "AS12876"},
		Status:   model.StatusTimeout,
	})

	st, body := e.admin(http.MethodGet, "/history", nil)
	mustStatus(t, st, http.StatusOK, body)
	item := decodeMap(t, body)["items"].([]any)[0].(map[string]any)
	if item["source_ip"] != "203.0.113.42" {
		t.Fatalf("source_ip %v", item["source_ip"])
	}
	geo, ok := item["geo"].(map[string]any)
	if !ok {
		t.Fatalf("geo must be an object, got %v", item["geo"])
	}
	if geo["country"] != "FR" || geo["city"] != "Paris" || geo["asn"] != "AS12876" || len(geo) != 3 {
		t.Fatalf("geo %v", geo)
	}
	// A timeout has no admin and no device: both null, not absent.
	for _, k := range []string{"decided_by_device", "decided_at"} {
		if v, ok := item[k]; !ok || v != nil {
			t.Fatalf("%s must be present and null, got %v", k, v)
		}
	}
}

func TestHistoryNewestFirst(t *testing.T) {
	e := newEnv(t)
	// Inserted out of order on purpose: the order must come from created_at.
	historyAdd(e, e.server, model.Request{Username: "second", CreatedAt: baseTime.Add(2 * time.Minute)})
	historyAdd(e, e.server, model.Request{Username: "oldest", CreatedAt: baseTime})
	historyAdd(e, e.server, model.Request{Username: "newest", CreatedAt: baseTime.Add(5 * time.Minute)})

	resp := historyGet(e, "")
	var got []string
	for _, it := range resp.Items {
		got = append(got, it.Username)
	}
	if strings.Join(got, ",") != "newest,second,oldest" {
		t.Fatalf("order %v", got)
	}
}

func TestHistoryFilterServer(t *testing.T) {
	e := newEnv(t)
	dbSrv, _ := e.enrollServer("db-01", model.OSLinux)
	historyAdd(e, e.server, model.Request{Username: "alice"})
	historyAdd(e, dbSrv, model.Request{Username: "bob"})

	resp := historyGet(e, "?server=db-01")
	if len(resp.Items) != 1 || resp.Items[0].Server != "db-01" || resp.Items[0].Username != "bob" {
		t.Fatalf("items %+v", resp.Items)
	}
	if resp := historyGet(e, "?server=unknown-99"); len(resp.Items) != 0 {
		t.Fatalf("unknown server must match nothing, got %+v", resp.Items)
	}
}

func TestHistoryFilterUsername(t *testing.T) {
	e := newEnv(t)
	historyAdd(e, e.server, model.Request{Username: "alice"})
	historyAdd(e, e.server, model.Request{Username: "bob"})
	historyAdd(e, e.server, model.Request{Username: "alice", Context: model.ContextSudo})

	resp := historyGet(e, "?username=alice")
	if len(resp.Items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(resp.Items), resp.Items)
	}
	for _, it := range resp.Items {
		if it.Username != "alice" {
			t.Fatalf("username %q leaked through the filter", it.Username)
		}
	}
}

func TestHistoryFilterContext(t *testing.T) {
	e := newEnv(t)
	historyAdd(e, e.server, model.Request{Username: "alice", Context: model.ContextSSH})
	historyAdd(e, e.server, model.Request{Username: "alice", Context: model.ContextSudo})

	resp := historyGet(e, "?context=sudo")
	if len(resp.Items) != 1 || resp.Items[0].Context != model.ContextSudo {
		t.Fatalf("items %+v", resp.Items)
	}
}

func TestHistoryFilterStatus(t *testing.T) {
	e := newEnv(t)
	historyAdd(e, e.server, model.Request{Username: "alice", Status: model.StatusApproved})
	historyAdd(e, e.server, model.Request{Username: "bob", Status: model.StatusDenied})
	historyAdd(e, e.server, model.Request{Username: "eve", Status: model.StatusBlockedIP})

	resp := historyGet(e, "?status=denied")
	if len(resp.Items) != 1 || resp.Items[0].Username != "bob" {
		t.Fatalf("items %+v", resp.Items)
	}
}

func TestHistoryPaging(t *testing.T) {
	e := newEnv(t)
	// Five requests one minute apart: newest is t+4m, oldest is t+0m.
	for i := 0; i < 5; i++ {
		historyAdd(e, e.server, model.Request{
			Username:  "u" + string(rune('0'+i)),
			CreatedAt: baseTime.Add(time.Duration(i) * time.Minute),
		})
	}

	page1 := historyGet(e, "?limit=2")
	if len(page1.Items) != 2 {
		t.Fatalf("page 1: %d items, want 2", len(page1.Items))
	}
	if page1.NextBefore == nil || !page1.NextBefore.Equal(page1.Items[1].CreatedAt) {
		t.Fatalf("page 1: next_before %v, want the 2nd item's created_at %v", page1.NextBefore, page1.Items[1].CreatedAt)
	}
	if !page1.Items[0].CreatedAt.Equal(baseTime.Add(4 * time.Minute)) {
		t.Fatalf("page 1 must start with the newest, got %v", page1.Items[0].CreatedAt)
	}

	before := url.QueryEscape(page1.NextBefore.Format(time.RFC3339Nano))
	page2 := historyGet(e, "?limit=2&before="+before)
	if len(page2.Items) != 2 {
		t.Fatalf("page 2: %d items, want 2", len(page2.Items))
	}
	if !page2.Items[0].CreatedAt.Equal(baseTime.Add(2*time.Minute)) || !page2.Items[1].CreatedAt.Equal(baseTime.Add(1*time.Minute)) {
		t.Fatalf("page 2 items %v %v", page2.Items[0].CreatedAt, page2.Items[1].CreatedAt)
	}
	if page2.NextBefore == nil {
		t.Fatalf("page 2 must have a next_before")
	}

	before = url.QueryEscape(page2.NextBefore.Format(time.RFC3339Nano))
	st, body := e.admin(http.MethodGet, "/history?limit=2&before="+before, nil)
	mustStatus(t, st, http.StatusOK, body)
	page3 := decode[model.HistoryResponse](t, body)
	if len(page3.Items) != 1 || !page3.Items[0].CreatedAt.Equal(baseTime) {
		t.Fatalf("page 3 items %+v", page3.Items)
	}
	if _, ok := decodeMap(t, body)["next_before"]; ok {
		t.Fatalf("last page must not carry next_before, body: %s", body)
	}
}

func TestHistoryLimitClamped(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		historyAdd(e, e.server, model.Request{Username: "alice", CreatedAt: baseTime.Add(time.Duration(i) * time.Second)})
	}
	st, body := e.admin(http.MethodGet, "/history?limit=500", nil)
	mustStatus(t, st, http.StatusOK, body)
	resp := decode[model.HistoryResponse](t, body)
	if len(resp.Items) != 3 {
		t.Fatalf("got %d items, want 3", len(resp.Items))
	}
	if _, ok := decodeMap(t, body)["next_before"]; ok {
		t.Fatalf("3 rows under a 200 cap is one page, body: %s", body)
	}
}

func TestHistoryBadQuery(t *testing.T) {
	e := newEnv(t)
	historyAdd(e, e.server, model.Request{Username: "alice"})
	cases := map[string]string{
		"limit zero":     "?limit=0",
		"limit negative": "?limit=-1",
		"limit text":     "?limit=abc",
		"limit decimal":  "?limit=1.5",
		"bad before":     "?before=yesterday",
		"bad context":    "?context=root",
		"bad status":     "?status=weird",
	}
	for name, query := range cases {
		st, body := e.admin(http.MethodGet, "/history"+query, nil)
		if st != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400; body: %s", name, st, body)
			continue
		}
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Errorf("%s: error body missing: %s", name, body)
		}
	}
}

func TestHistoryServerTokenForbidden(t *testing.T) {
	e := newEnv(t)
	st, body := e.agent(http.MethodGet, "/history", nil)
	mustStatus(t, st, http.StatusForbidden, body)
}
