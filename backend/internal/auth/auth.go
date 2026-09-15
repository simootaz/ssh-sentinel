// Package auth checks bearer tokens: the per-server tokens of the agents and
// the admin token shared by the phones (docs/architecture.md, section 6).
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Errors returned by Authenticate. Handlers answer 401 for both.
var (
	ErrNoToken      = errors.New("missing bearer token")
	ErrInvalidToken = errors.New("invalid token")
)

// Kind says which credential authenticated the call.
type Kind int

const (
	KindAdmin  Kind = iota + 1 // the shared admin token: the phones
	KindServer                 // a per-server token: an agent
)

// Principal is who is calling. Server is set for KindServer only.
type Principal struct {
	Kind   Kind
	Server *model.Server
}

// HashToken returns the hex sha256 of a token. Only hashes are stored and compared.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Authenticator resolves bearer tokens to principals.
type Authenticator struct {
	store     db.Store
	adminHash string
}

// New builds an authenticator. An empty adminToken disables admin access.
func New(store db.Store, adminToken string) *Authenticator {
	a := &Authenticator{store: store}
	if adminToken != "" {
		a.adminHash = HashToken(adminToken)
	}
	return a
}

// BearerToken extracts the token of an "Authorization: Bearer <token>" header.
func BearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	return tok, tok != ""
}

// Authenticate identifies the caller of r. Both comparisons are on sha256
// hashes and in constant time, so neither the admin secret nor a server token
// can be guessed byte by byte from response timings.
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (*Principal, error) {
	tok, ok := BearerToken(r)
	if !ok {
		return nil, ErrNoToken
	}
	hash := HashToken(tok)

	if a.adminHash != "" && subtle.ConstantTimeCompare([]byte(hash), []byte(a.adminHash)) == 1 {
		return &Principal{Kind: KindAdmin}, nil
	}

	srv, err := a.store.ServerByTokenHash(ctx, hash)
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	// The lookup already matched on the hash; the constant-time compare is a
	// second check that does not depend on how the database compared strings.
	if subtle.ConstantTimeCompare([]byte(hash), []byte(srv.TokenHash)) != 1 {
		return nil, ErrInvalidToken
	}
	return &Principal{Kind: KindServer, Server: srv}, nil
}

type ctxKey struct{}

// WithPrincipal stores p in ctx for the handler behind the auth middleware.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the principal stored by WithPrincipal, or nil.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}
