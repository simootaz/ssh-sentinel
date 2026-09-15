package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Paging bounds of GET /history (docs/architecture.md, section 5).
const (
	historyDefaultLimit = 50
	historyMaxLimit     = 200
)

// history answers GET /history: requests newest first, filtered by the
// optional query parameters, paged with before / next_before.
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := historyDefaultLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		// Above the cap the app simply gets the cap: no reason to fail a
		// call that only asked for more than we hand out.
		limit = min(n, historyMaxLimit)
	}

	var before *time.Time
	if s := q.Get("before"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "before must be an RFC 3339 timestamp")
			return
		}
		t = t.UTC()
		before = &t
	}

	reqContext := q.Get("context")
	if reqContext != "" && !model.ValidContext(reqContext) {
		writeError(w, http.StatusBadRequest, "context must be ssh or sudo")
		return
	}
	status := q.Get("status")
	if status != "" && !model.ValidStatus(status) {
		writeError(w, http.StatusBadRequest, "unknown status")
		return
	}

	// Ask for one row more than the page: if it comes back there is a next
	// page, and it is dropped before answering. One query instead of a
	// count plus a select.
	rows, err := h.store.ListRequests(r.Context(), db.RequestFilter{
		Limit:    limit + 1,
		Before:   before,
		Server:   q.Get("server"),
		Username: q.Get("username"),
		Context:  reqContext,
		Status:   status,
	})
	if err != nil {
		h.log.Error("history: list requests", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// items is allocated even when empty so the JSON is [] and never null.
	resp := model.HistoryResponse{Items: make([]model.HistoryItem, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[len(rows)-1].CreatedAt
		resp.NextBefore = &next
	}
	for i := range rows {
		resp.Items = append(resp.Items, model.ToHistoryItem(&rows[i]))
	}
	writeJSON(w, http.StatusOK, resp)
}
