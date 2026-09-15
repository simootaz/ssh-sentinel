package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// enrollServer inserts a server row and returns it with its plain token.
func (e *env) enrollServer(name, os string) (*model.Server, string) {
	e.t.Helper()
	token := "server-token-" + name
	s, err := e.store.CreateServer(context.Background(), name, auth.HashToken(token), os, e.clock.Now())
	if err != nil {
		e.t.Fatalf("enroll %s: %v", name, err)
	}
	return s, token
}

// addDevice registers a phone with the given label and returns its row.
func (e *env) addDevice(label string) *model.Device {
	e.t.Helper()
	d, _, err := e.store.UpsertDevice(context.Background(), "fcm-token-"+label, model.PlatformAndroid, &label, e.clock.Now())
	if err != nil {
		e.t.Fatalf("add device: %v", err)
	}
	return d
}

// do sends one request. body nil means no body; a []byte is sent as is;
// anything else is JSON-encoded. token "" sends no Authorization header.
func (e *env) do(method, path, token string, body any) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// admin sends a request with the admin token.
func (e *env) admin(method, path string, body any) (int, []byte) {
	e.t.Helper()
	return e.do(method, path, testAdminToken, body)
}

// agent sends a request with the token of the default server web-01.
func (e *env) agent(method, path string, body any) (int, []byte) {
	e.t.Helper()
	return e.do(method, path, e.serverToken, body)
}

// decode parses a JSON body into T.
func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return v
}

// decodeMap parses a JSON object into a map, for checking exact keys and nulls.
func decodeMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	return decode[map[string]any](t, body)
}

func strp(s string) *string { return &s }

// mustStatus fails the test with the body when the status differs.
func mustStatus(t *testing.T, got, want int, body []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status %d, want %d; body: %s", got, want, body)
	}
}
