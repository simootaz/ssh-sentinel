package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// whitelistMaxTTL caps ttl_seconds at a century. A TTL beyond that is a
// typo, not a policy, and would overflow the duration arithmetic.
const whitelistMaxTTL = int64(100 * 365 * 24 * 60 * 60)

// listWhitelist answers GET /whitelist. The admin token gets every unexpired
// entry; a server token gets that server's entries plus the global ones and
// nothing else (docs/architecture.md, sections 5 and 6). The store drops
// expired rows on the way, which is the lazy deletion the contract promises.
func (h *Handler) listWhitelist(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	if p == nil {
		// requireAny always sets it. If it ever did not, refuse rather than
		// hand the whole list to an unidentified caller.
		writeError(w, http.StatusUnauthorized, "missing or invalid token")
		return
	}
	var serverID *string
	if p.Kind == auth.KindServer {
		serverID = &p.Server.ID
	}

	rows, err := h.store.ListWhitelist(r.Context(), serverID, h.clock.Now())
	if err != nil {
		h.log.Error("whitelist: list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// items is allocated even when empty so the JSON is [] and never null.
	items := make([]model.WhitelistItem, 0, len(rows))
	for i := range rows {
		items = append(items, model.ToWhitelistItem(&rows[i]))
	}
	writeJSON(w, http.StatusOK, model.ListResponse[model.WhitelistItem]{Items: items})
}

// createWhitelist answers POST /whitelist: 201 with the new entry, or 200
// when an entry for the same (username, context, server) already existed
// and only had its expiry refreshed.
func (h *Handler) createWhitelist(w http.ResponseWriter, r *http.Request) {
	var body model.WhitelistBody
	if !decodeJSON(w, r, &body) {
		return
	}

	username := strings.TrimSpace(body.Username)
	if username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	reqContext := strings.TrimSpace(body.Context)
	if reqContext == "" {
		reqContext = model.ContextSSH
	}
	if !model.ValidContext(reqContext) {
		writeError(w, http.StatusBadRequest, "context must be ssh or sudo")
		return
	}
	if body.TTLSeconds != nil && (*body.TTLSeconds <= 0 || *body.TTLSeconds > whitelistMaxTTL) {
		writeError(w, http.StatusBadRequest, "ttl_seconds must be between 1 and 3153600000")
		return
	}

	now := h.clock.Now()
	entry := &model.WhitelistEntry{Username: username, Context: reqContext}

	// server null, absent or empty means every server; otherwise the name
	// must match an enrolled server.
	if body.Server != nil && strings.TrimSpace(*body.Server) != "" {
		srv, err := h.store.ServerByName(r.Context(), strings.TrimSpace(*body.Server))
		if errNotFound(err) {
			writeError(w, http.StatusNotFound, "unknown server")
			return
		}
		if err != nil {
			h.log.Error("whitelist: server lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		entry.ServerID = &srv.ID
	}

	// ttl_seconds absent or null means permanent (expires_at stays nil).
	if body.TTLSeconds != nil {
		exp := now.Add(time.Duration(*body.TTLSeconds) * time.Second)
		entry.ExpiresAt = &exp
	}

	saved, created, err := h.store.UpsertWhitelist(r.Context(), entry, now)
	if err != nil {
		h.log.Error("whitelist: upsert", "username", username, "context", reqContext, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, model.ToWhitelistItem(saved))
}

// deleteWhitelist answers DELETE /whitelist/{id}: 204 with no body, or 404.
func (h *Handler) deleteWhitelist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := h.store.DeleteWhitelist(r.Context(), id)
	if errNotFound(err) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.log.Error("whitelist: delete", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
