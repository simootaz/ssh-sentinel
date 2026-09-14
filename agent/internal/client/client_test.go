package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// capture records what the backend received and answers with a fixed body.
type capture struct {
	method, path string
	header       http.Header
	body         map[string]any
}

func backend(t *testing.T, status int, reply string) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.method, c.path, c.header = r.Method, r.URL.Path, r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &c.body); err != nil {
				t.Errorf("request body is not JSON: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func TestAccessRequestSSH(t *testing.T) {
	srv, got := backend(t, 200, `{"request_id":"req-1","verdict":"approve","reason":"admin","decided_at":"2026-09-14T20:12:07Z"}`)
	ip := "203.0.113.42"
	c := New(srv.URL+"/", "tok")

	resp, err := c.AccessRequest(context.Background(), AccessRequest{
		Context: "ssh", Mode: "enforce", Username: "deploy", SourceIP: &ip, Hostname: "web-01", TTY: "ssh",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.method != http.MethodPost || got.path != "/access-request" {
		t.Errorf("got %s %s, want POST /access-request", got.method, got.path)
	}
	if got.header.Get("Authorization") != "Bearer tok" {
		t.Errorf("Authorization = %q", got.header.Get("Authorization"))
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", got.header.Get("Content-Type"))
	}
	want := map[string]any{
		"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42",
		"hostname": "web-01", "tty": "ssh", "command": nil,
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %v, want %v", got.body, want)
	}
	if resp.RequestID != "req-1" || resp.Verdict != "approve" || resp.Reason != "admin" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.DecidedAt == nil || *resp.DecidedAt != "2026-09-14T20:12:07Z" {
		t.Errorf("DecidedAt = %v", resp.DecidedAt)
	}
}

func TestAccessRequestSudoSendsNulls(t *testing.T) {
	srv, got := backend(t, 200, `{"request_id":"req-2","verdict":"deny","reason":"timeout","decided_at":null}`)
	cmd := "sudo systemctl restart nginx"
	c := New(srv.URL, "tok")

	resp, err := c.AccessRequest(context.Background(), AccessRequest{
		Context: "sudo", Mode: "enforce", Username: "deploy", Hostname: "web-01", TTY: "pts/0", Command: &cmd,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{
		"context": "sudo", "mode": "enforce", "username": "deploy", "source_ip": nil,
		"hostname": "web-01", "tty": "pts/0", "command": cmd,
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %v, want %v", got.body, want)
	}
	if resp.Verdict != "deny" || resp.Reason != "timeout" || resp.DecidedAt != nil {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestWhitelist(t *testing.T) {
	srv, got := backend(t, 200, `{"items": [
		{"id": "9a7d", "username": "deploy", "context": "sudo", "server": "web-01",
		 "expires_at": "2026-09-14T21:12:07Z", "created_at": "2026-09-14T20:12:07Z",
		 "created_from_request": "5f1c", "created_by_device": "Pixel 8"},
		{"id": "c1d2", "username": "ansible", "context": "ssh", "server": null,
		 "expires_at": null, "created_at": "2026-09-10T08:00:00Z",
		 "created_from_request": null, "created_by_device": null}
	]}`)
	c := New(srv.URL, "tok")

	items, err := c.Whitelist(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.method != http.MethodGet || got.path != "/whitelist" || got.header.Get("Authorization") != "Bearer tok" {
		t.Errorf("got %s %s auth %q", got.method, got.path, got.header.Get("Authorization"))
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	first := items[0]
	wantExp := time.Date(2026, 9, 14, 21, 12, 7, 0, time.UTC)
	if first.Username != "deploy" || first.Context != "sudo" || first.Server == nil || *first.Server != "web-01" {
		t.Errorf("first entry = %+v", first)
	}
	if first.ExpiresAt == nil || !first.ExpiresAt.Equal(wantExp) {
		t.Errorf("first ExpiresAt = %v, want %v", first.ExpiresAt, wantExp)
	}
	second := items[1]
	if second.Username != "ansible" || second.Server != nil || second.ExpiresAt != nil {
		t.Errorf("second entry = %+v", second)
	}
}
