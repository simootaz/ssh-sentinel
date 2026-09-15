package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// health answers GET /healthz: no auth, 200 when the database answers, 503 otherwise.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.store.Ping(ctx); err != nil {
		h.log.Error("healthz: database", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, model.HealthResponse{Status: "degraded", DB: "error"})
		return
	}
	writeJSON(w, http.StatusOK, model.HealthResponse{Status: "ok", DB: "ok"})
}
