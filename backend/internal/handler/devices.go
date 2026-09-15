package handler

import (
	"net/http"
	"strings"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// The /devices routes manage the admin phones (docs/architecture.md,
// section 5). The app calls POST /devices at first launch and whenever FCM
// rotates its token; the FCM token is the upsert key and is never returned,
// so a leaked API answer cannot be used to push to a phone.

// createDevice answers POST /devices: 201 when the token is new, 200 when it
// was already known and its platform, label and last_seen_at were refreshed.
func (h *Handler) createDevice(w http.ResponseWriter, r *http.Request) {
	var body model.DeviceBody
	if !decodeJSON(w, r, &body) {
		return
	}
	token := strings.TrimSpace(body.FCMToken)
	if token == "" {
		writeError(w, http.StatusBadRequest, "fcm_token is required")
		return
	}
	if !model.ValidPlatform(body.Platform) {
		writeError(w, http.StatusBadRequest, "platform must be android or ios")
		return
	}

	dev, created, err := h.store.UpsertDevice(r.Context(), token, body.Platform, optionalText(body.Label), h.clock.Now())
	if err != nil {
		h.log.Error("devices: upsert", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, model.ToDeviceItem(dev))
}

// listDevices answers GET /devices: every phone, oldest first.
func (h *Handler) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := h.store.ListDevices(r.Context())
	if err != nil {
		h.log.Error("devices: list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]model.DeviceItem, 0, len(devices)) // never null in the JSON
	for i := range devices {
		items = append(items, model.ToDeviceItem(&devices[i]))
	}
	writeJSON(w, http.StatusOK, model.ListResponse[model.DeviceItem]{Items: items})
}

// deleteDevice answers DELETE /devices/{id}: 204, or 404 when the id is
// unknown, malformed or already deleted. The phone stops receiving pushes at
// once; the row is kept with deleted_at set so history rows keep their label.
func (h *Handler) deleteDevice(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := h.store.DeleteDevice(r.Context(), id, h.clock.Now())
	switch {
	case errNotFound(err):
		writeError(w, http.StatusNotFound, "device not found")
	case err != nil:
		h.log.Error("devices: delete", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// optionalText trims a free-text field of the request body. Absent, empty or
// blank all become nil, so the database stores NULL and the API answers null.
func optionalText(p *string) *string {
	if p == nil {
		return nil
	}
	s := strings.TrimSpace(*p)
	if s == "" {
		return nil
	}
	return &s
}
