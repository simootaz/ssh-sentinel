package handler

import (
	"net/http"
	"testing"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

func TestHealthOK(t *testing.T) {
	e := newEnv(t)
	st, body := e.do(http.MethodGet, "/healthz", "", nil)
	mustStatus(t, st, http.StatusOK, body)
	h := decode[model.HealthResponse](t, body)
	if h.Status != "ok" || h.DB != "ok" {
		t.Fatalf("body %s", body)
	}
}

func TestHealthDegraded(t *testing.T) {
	e := newEnv(t)
	e.store.PingErr = http.ErrServerClosed
	st, body := e.do(http.MethodGet, "/healthz", "", nil)
	mustStatus(t, st, http.StatusServiceUnavailable, body)
	h := decode[model.HealthResponse](t, body)
	if h.Status != "degraded" || h.DB != "error" {
		t.Fatalf("body %s", body)
	}
}

func TestUnknownRouteIsJSON404(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodGet, "/nope", nil)
	mustStatus(t, st, http.StatusNotFound, body)
	if decode[model.ErrorResponse](t, body).Error == "" {
		t.Fatalf("body %s", body)
	}
}

func TestWrongMethodIsJSON405(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPut, "/history", nil)
	mustStatus(t, st, http.StatusMethodNotAllowed, body)
	if decode[model.ErrorResponse](t, body).Error == "" {
		t.Fatalf("body %s", body)
	}
}

func TestAuthMissingToken(t *testing.T) {
	e := newEnv(t)
	st, body := e.do(http.MethodGet, "/history", "", nil)
	mustStatus(t, st, http.StatusUnauthorized, body)
}

func TestAuthInvalidToken(t *testing.T) {
	e := newEnv(t)
	st, body := e.do(http.MethodGet, "/history", "wrong", nil)
	mustStatus(t, st, http.StatusUnauthorized, body)
}

func TestAuthServerTokenOnAdminRoute(t *testing.T) {
	e := newEnv(t)
	st, body := e.agent(http.MethodGet, "/history", nil)
	mustStatus(t, st, http.StatusForbidden, body)
}

func TestAuthAdminTokenOnAgentRoute(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/access-request", model.AccessRequestBody{Username: "x"})
	mustStatus(t, st, http.StatusForbidden, body)
}
