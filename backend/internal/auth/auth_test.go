package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/db/dbtest"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

const (
	adminToken  = "admin-secret-token"
	serverToken = "srv-web-01-token"
)

var now = time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)

func TestHashTokenKnownVector(t *testing.T) {
	// sha256("abc"), the FIPS 180 example vector.
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := auth.HashToken("abc"); got != want {
		t.Fatalf("HashToken(abc) = %s, want %s", got, want)
	}
	if got := auth.HashToken(""); len(got) != 64 {
		t.Errorf("HashToken(\"\") = %q, want 64 hex characters", got)
	}
}

func TestBearerToken(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   string
		ok     bool
	}{
		{"missing header", "", "", false},
		{"basic auth", "Basic dXNlcjpwYXNz", "", false},
		{"bearer", "Bearer abc123", "abc123", true},
		{"lower-case scheme", "bearer abc123", "abc123", true},
		{"upper-case scheme", "BEARER abc123", "abc123", true},
		{"empty token", "Bearer ", "", false},
		{"blank token", "Bearer    ", "", false},
		{"surrounding spaces trimmed", "Bearer   abc123   ", "abc123", true},
		{"scheme glued to the token", "Bearerabc123", "", false},
		{"scheme only", "Bearer", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/history", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			got, ok := auth.BearerToken(r)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("BearerToken(%q) = (%q, %v), want (%q, %v)", tc.header, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// request builds a request carrying token as a bearer; "" sends no header.
func request(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/access-request", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// setup returns a store with web-01 enrolled and an authenticator with the
// admin token configured.
func setup(t *testing.T) (*dbtest.Memory, *auth.Authenticator, *model.Server) {
	t.Helper()
	store := dbtest.New()
	srv, err := store.CreateServer(context.Background(), "web-01", auth.HashToken(serverToken), model.OSLinux, now)
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	return store, auth.New(store, adminToken), srv
}

func TestAuthenticateAdmin(t *testing.T) {
	_, a, _ := setup(t)
	p, err := a.Authenticate(context.Background(), request(adminToken))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if p.Kind != auth.KindAdmin {
		t.Errorf("kind = %v, want KindAdmin", p.Kind)
	}
	if p.Server != nil {
		t.Errorf("an admin principal has no server, got %+v", p.Server)
	}
}

func TestAuthenticateServer(t *testing.T) {
	_, a, srv := setup(t)
	p, err := a.Authenticate(context.Background(), request(serverToken))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if p.Kind != auth.KindServer {
		t.Errorf("kind = %v, want KindServer", p.Kind)
	}
	if p.Server == nil || p.Server.ID != srv.ID || p.Server.Name != "web-01" {
		t.Fatalf("server = %+v, want %s (%s)", p.Server, srv.Name, srv.ID)
	}
	if p.Server.TokenHash != auth.HashToken(serverToken) {
		t.Errorf("server row carries hash %s, want the token's hash", p.Server.TokenHash)
	}
}

func TestAuthenticateUnknownToken(t *testing.T) {
	_, a, _ := setup(t)
	for _, tok := range []string{"nope", serverToken + "x", auth.HashToken(serverToken)} {
		p, err := a.Authenticate(context.Background(), request(tok))
		if !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("token %q: err = %v, want ErrInvalidToken", tok, err)
		}
		if p != nil {
			t.Errorf("token %q: principal = %+v, want nil", tok, p)
		}
	}
}

func TestAuthenticateNoToken(t *testing.T) {
	_, a, _ := setup(t)
	cases := map[string]*http.Request{
		"no header": request(""),
		"basic":     withHeader("Basic dXNlcjpwYXNz"),
		"empty":     withHeader("Bearer "),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := a.Authenticate(context.Background(), r)
			if !errors.Is(err, auth.ErrNoToken) {
				t.Fatalf("err = %v, want ErrNoToken", err)
			}
			if p != nil {
				t.Fatalf("principal = %+v, want nil", p)
			}
		})
	}
}

func withHeader(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/history", nil)
	r.Header.Set("Authorization", value)
	return r
}

func TestAuthenticateAdminDisabled(t *testing.T) {
	store, _, _ := setup(t)
	a := auth.New(store, "")

	if _, err := a.Authenticate(context.Background(), request(adminToken)); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("admin token with admin access disabled: err = %v, want ErrInvalidToken", err)
	}
	// Server tokens keep working.
	p, err := a.Authenticate(context.Background(), request(serverToken))
	if err != nil || p.Kind != auth.KindServer {
		t.Errorf("server token: principal %+v, err %v; want KindServer", p, err)
	}
}

// failingStore is a store whose token lookup fails, as a database outage would.
type failingStore struct {
	db.Store
	err error
}

func (f failingStore) ServerByTokenHash(context.Context, string) (*model.Server, error) {
	return nil, f.err
}

func TestAuthenticateStoreError(t *testing.T) {
	dbDown := errors.New("connection refused")
	a := auth.New(failingStore{Store: dbtest.New(), err: dbDown}, adminToken)

	// A database outage is not an invalid token: the handler must answer 500, not 401.
	_, err := a.Authenticate(context.Background(), request(serverToken))
	if !errors.Is(err, dbDown) {
		t.Fatalf("err = %v, want the store error", err)
	}
	if errors.Is(err, auth.ErrInvalidToken) || errors.Is(err, auth.ErrNoToken) {
		t.Fatalf("store error must not be reported as a token error: %v", err)
	}

	// The admin token needs no database and still works.
	p, err := a.Authenticate(context.Background(), request(adminToken))
	if err != nil || p.Kind != auth.KindAdmin {
		t.Fatalf("admin with the database down: principal %+v, err %v; want KindAdmin", p, err)
	}
}

func TestPrincipalContextRoundTrip(t *testing.T) {
	if got := auth.PrincipalFrom(context.Background()); got != nil {
		t.Fatalf("PrincipalFrom(empty context) = %+v, want nil", got)
	}
	p := &auth.Principal{Kind: auth.KindServer, Server: &model.Server{ID: "srv-1", Name: "web-01"}}
	ctx := auth.WithPrincipal(context.Background(), p)
	if got := auth.PrincipalFrom(ctx); got != p {
		t.Fatalf("PrincipalFrom = %+v, want the same principal %+v", got, p)
	}
}
