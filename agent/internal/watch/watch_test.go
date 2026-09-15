package watch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/client"
)

// scripted hands out one batch per call and can cancel the context after n calls.
type scripted struct {
	batches     [][]Event
	err         error
	calls       int
	cancelAfter int
	cancel      context.CancelFunc
}

func (s *scripted) Next(ctx context.Context) ([]Event, error) {
	s.calls++
	if s.cancel != nil && s.calls >= s.cancelAfter {
		s.cancel()
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.calls-1 < len(s.batches) {
		return s.batches[s.calls-1], nil
	}
	return nil, nil
}

func login(user, ip string) Event {
	return Event{RecordID: 1, Username: user, SourceIP: ip, Method: "publickey"}
}

func TestRunOnceReportsInNotifyMode(t *testing.T) {
	var body map[string]any
	var method, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		io.WriteString(w, `{"request_id":"r1","verdict":"approve","reason":"notify","decided_at":"2026-09-14T20:11:41Z"}`)
	}))
	defer srv.Close()

	rec := &auditlog.Recorder{}
	w := &Watcher{Source: &scripted{batches: [][]Event{{login("deploy", "203.0.113.42")}}}, Client: client.New(srv.URL, "tok"), Hostname: "win-01", Log: rec}
	n, err := w.RunOnce(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("RunOnce = %d, %v", n, err)
	}
	if method != http.MethodPost || path != "/access-request" || auth != "Bearer tok" {
		t.Errorf("got %s %s auth %q", method, path, auth)
	}
	want := map[string]any{
		"context": "ssh", "mode": "notify", "username": "deploy", "source_ip": "203.0.113.42",
		"hostname": "win-01", "tty": "ssh", "command": nil,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body\n got %v\nwant %v", body, want)
	}
	wantLine := auditlog.Line{Decision: "allow", Context: "ssh", User: "deploy", RHost: "203.0.113.42", Host: "win-01", Reason: "notify", Request: "r1"}
	if got := rec.Last(); got != wantLine {
		t.Errorf("line\n got %s\nwant %s", got, wantLine)
	}
}

func TestRunOnceBackendFailuresStillAllow(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reason string
	}{
		{"500", 500, "unreachable"},
		{"401", 401, "rejected"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, `{"error":"no"}`)
			}))
			defer srv.Close()
			rec := &auditlog.Recorder{}
			w := &Watcher{Source: &scripted{batches: [][]Event{{login("deploy", "203.0.113.42")}}}, Client: client.New(srv.URL, "tok"), Hostname: "win-01", Log: rec}
			if n, err := w.RunOnce(context.Background()); err != nil || n != 1 {
				t.Fatalf("RunOnce = %d, %v", n, err)
			}
			got := rec.Last()
			if got.Decision != "allow" || got.Reason != tc.reason || got.Request != "" {
				t.Errorf("line = %s", got)
			}
		})
	}
}

func TestRunOnceSourceError(t *testing.T) {
	w := &Watcher{Source: &scripted{err: errors.New("wevtutil: channel not found")}, Log: &auditlog.Recorder{}}
	if n, err := w.RunOnce(context.Background()); err == nil || n != 0 {
		t.Fatalf("RunOnce = %d, %v; want the source error", n, err)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"request_id":"r1","verdict":"approve","reason":"notify"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &scripted{batches: [][]Event{{login("a", "1.1.1.1"), login("b", "2.2.2.2")}}, cancelAfter: 2, cancel: cancel}
	rec := &auditlog.Recorder{}
	w := &Watcher{Source: src, Client: client.New(srv.URL, "tok"), Hostname: "win-01", Log: rec, Poll: 10 * time.Millisecond}

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after the context was cancelled")
	}
	if len(rec.Lines) != 2 || src.calls < 2 {
		t.Errorf("reported %d logins over %d polls", len(rec.Lines), src.calls)
	}
}
