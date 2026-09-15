package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// The /geo-rules routes manage the country blocklist (docs/architecture.md,
// section 5). One rule per country; requests whose geo lookup lands in a
// listed country are denied without a push.

// listGeoRules answers GET /geo-rules, ordered by country code.
func (h *Handler) listGeoRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListGeoRules(r.Context())
	if err != nil {
		h.log.Error("geo-rules: list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]model.GeoRuleItem, 0, len(rules)) // never null in the JSON
	for i := range rules {
		items = append(items, model.ToGeoRuleItem(&rules[i]))
	}
	writeJSON(w, http.StatusOK, model.ListResponse[model.GeoRuleItem]{Items: items})
}

// createGeoRule answers POST /geo-rules: 201 with the rule, 400 for a code
// that is not an assigned ISO 3166-1 alpha-2 code, 409 when the country
// already has a rule. The code is accepted in any case and stored upper case,
// which is also how the rules engine compares it with the geo lookup.
func (h *Handler) createGeoRule(w http.ResponseWriter, r *http.Request) {
	var body model.GeoRuleBody
	if !decodeJSON(w, r, &body) {
		return
	}
	country := strings.ToUpper(strings.TrimSpace(body.Country))
	if !model.ValidCountry(country) {
		writeError(w, http.StatusBadRequest, "unknown country code")
		return
	}

	rule, err := h.store.CreateGeoRule(r.Context(), country, optionalText(body.Note), h.clock.Now())
	switch {
	case errors.Is(err, db.ErrConflict):
		writeError(w, http.StatusConflict, "country already has a rule")
	case err != nil:
		h.log.Error("geo-rules: create", "country", country, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusCreated, model.ToGeoRuleItem(rule))
	}
}

// deleteGeoRule answers DELETE /geo-rules/{id}: 204, or 404 when the id is
// unknown or malformed.
func (h *Handler) deleteGeoRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := h.store.DeleteGeoRule(r.Context(), id)
	switch {
	case errNotFound(err):
		writeError(w, http.StatusNotFound, "geo rule not found")
	case err != nil:
		h.log.Error("geo-rules: delete", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
