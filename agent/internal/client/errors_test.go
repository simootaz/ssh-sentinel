package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAccessRequestErrorClasses(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		reply    string
		rejected bool
		message  string
	}{
		{"401 with message", 401, `{"error":"bad token"}`, true, "bad token"},
		{"400 without message", 400, `{}`, true, "no error message"},
		{"403", 403, `{"error":"not allowed"}`, true, "not allowed"},
		{"500", 500, `boom`, false, ""},
		{"502 html", 502, `<html>bad gateway</html>`, false, ""},
		{"302", 302, ``, false, ""},
		{"malformed json", 200, `{"verdict":`, false, ""},
		{"unknown verdict", 200, `{"request_id":"x","verdict":"maybe","reason":"admin"}`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := backend(t, tc.status, tc.reply)
			c := New(srv.URL, "tok")
			_, err := c.AccessRequest(context.Background(), AccessRequest{Context: "ssh", Mode: "enforce", Username: "u"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if IsRejected(err) != tc.rejected || IsUnreachable(err) == tc.rejected {
				t.Fatalf("rejected=%v unreachable=%v for %v", IsRejected(err), IsUnreachable(err), err)
			}
			if tc.rejected {
				r := err.(*RejectedError)
				if r.Status != tc.status || r.Message != tc.message {
					t.Errorf("got %d %q, want %d %q", r.Status, r.Message, tc.status, tc.message)
				}
			}
		})
	}
}

func TestAccessRequestConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := New(url, "tok").AccessRequest(context.Background(), AccessRequest{Username: "u"})
	if !IsUnreachable(err) {
		t.Fatalf("got %v, want an unreachable error", err)
	}
}

func TestAccessRequestHonoursContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			io.WriteString(w, `{"verdict":"approve"}`)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := New(srv.URL, "tok").AccessRequest(ctx, AccessRequest{Username: "u"})
	if !IsUnreachable(err) {
		t.Fatalf("got %v, want an unreachable error", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("call took %v, the context deadline was not honoured", time.Since(start))
	}
}

func TestWhitelistRejected(t *testing.T) {
	srv, _ := backend(t, 401, `{"error":"unknown server"}`)
	_, err := New(srv.URL, "tok").Whitelist(context.Background())
	if !IsRejected(err) {
		t.Fatalf("got %v, want a rejected error", err)
	}
}
