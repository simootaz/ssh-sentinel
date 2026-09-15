package handler

import (
	"net/http"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// The /blocked-ips routes show and lift the auto-blocks (docs/architecture.md,
// section 5). Rows are inserted by the rules engine after repeated denials,
// never through the API; the app can only list and unblock.

// listBlockedIPs answers GET /blocked-ips: the blocks in force right now,
// newest first. Expired and unblocked rows are left out.
func (h *Handler) listBlockedIPs(w http.ResponseWriter, r *http.Request) {
	blocks, err := h.store.ListBlockedIPs(r.Context(), h.clock.Now())
	if err != nil {
		h.log.Error("blocked-ips: list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]model.BlockedIPItem, 0, len(blocks)) // never null in the JSON
	for i := range blocks {
		items = append(items, model.ToBlockedIPItem(&blocks[i]))
	}
	writeJSON(w, http.StatusOK, model.ListResponse[model.BlockedIPItem]{Items: items})
}

// deleteBlockedIP answers DELETE /blocked-ips/{id}: 204, or 404 when the id
// is unknown, malformed, or the block is already over. Unblocking sets
// expires_at to now instead of deleting the row, so that the denials that
// caused this block are not counted again: three new denials are needed to
// block the IP a second time.
func (h *Handler) deleteBlockedIP(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := h.store.UnblockIP(r.Context(), id, h.clock.Now())
	switch {
	case errNotFound(err):
		writeError(w, http.StatusNotFound, "blocked ip not found")
	case err != nil:
		h.log.Error("blocked-ips: unblock", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
