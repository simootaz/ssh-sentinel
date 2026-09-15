package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// maxTTLSeconds is the largest ttl_seconds a time.Duration can hold (about
// 292 years). Anything above would wrap around and expire the entry at once.
const maxTTLSeconds = math.MaxInt64 / int64(time.Second)

// verdict answers POST /verdict: an admin decides a pending request from the
// app (docs/architecture.md, section 2 step 10 and section 5). The store
// makes the update conditional on the row still being pending and unexpired,
// so the first verdict wins and every later one gets 409 with the outcome.
func (h *Handler) verdict(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body model.VerdictBody
	if !decodeJSON(w, r, &body) {
		return
	}
	id := strings.ToLower(body.RequestID)
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "request_id must be a UUID")
		return
	}

	var status string
	switch body.Verdict {
	case model.VerdictApprove, model.VerdictApproveAlways:
		status = model.StatusApproved
	case model.VerdictDeny:
		status = model.StatusDenied
	default:
		writeError(w, http.StatusBadRequest, "verdict must be approve, deny or approve_always")
		return
	}

	// ttl_seconds is only read with approve_always; the other verdicts ignore
	// it even when present. Absent or null means a permanent entry.
	var ttl time.Duration
	if body.Verdict == model.VerdictApproveAlways && body.TTLSeconds != nil {
		switch {
		case *body.TTLSeconds <= 0:
			writeError(w, http.StatusBadRequest, "ttl_seconds must be greater than 0")
			return
		case *body.TTLSeconds > maxTTLSeconds:
			writeError(w, http.StatusBadRequest, "ttl_seconds is too large")
			return
		}
		ttl = time.Duration(*body.TTLSeconds) * time.Second
	}

	// device_id is attribution, not authentication (section 6): the admin
	// token is what authorizes the verdict. An unknown id means the phone's
	// row was deleted; the verdict still goes through, with no device.
	var deviceID *string
	if body.DeviceID != nil {
		did := strings.ToLower(*body.DeviceID)
		if !isUUID(did) {
			writeError(w, http.StatusBadRequest, "device_id must be a UUID")
			return
		}
		dev, err := h.store.DeviceByID(ctx, did)
		switch {
		case errNotFound(err):
			h.log.Warn("verdict: unknown device, recording the verdict without it", "request", id, "device", did)
		case err != nil:
			h.log.Error("verdict: device lookup", "request", id, "device", did, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		default:
			deviceID = &dev.ID
		}
	}

	now := h.clock.Now()
	row, err := h.store.DecideRequest(ctx, id, status, deviceID, now)
	switch {
	case errNotFound(err):
		writeError(w, http.StatusNotFound, "request not found")
		return
	case errors.Is(err, db.ErrAlreadyDecided):
		h.verdictConflict(ctx, w, id, now)
		return
	case err != nil:
		h.log.Error("verdict: decide request", "request", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.log.Info("verdict", "request", row.ID, "status", row.Status, "device", deref(row.DecidedByDeviceLabel))

	// The decision is final from here on: tell every phone now, so the
	// notification is dismissed even if the bookkeeping below fails. The
	// deciding phone gets the push too and simply ignores it.
	h.pushAll(ctx, row.ID, model.DecidedPush(row))

	resp := model.VerdictResponse{
		RequestID:       row.ID,
		Status:          row.Status,
		DecidedAt:       row.DecidedAt,
		DecidedByDevice: row.DecidedByDeviceLabel,
	}

	if body.Verdict == model.VerdictApproveAlways {
		// The entry is for this user, this context and this server only:
		// always-allow on a sudo request whitelists sudo, not ssh (section 8, item 7).
		entry := &model.WhitelistEntry{
			Username:        row.Username,
			Context:         row.Context,
			ServerID:        &row.ServerID,
			CreatedFrom:     &row.ID,
			CreatedByDevice: deviceID,
		}
		if ttl > 0 {
			exp := now.Add(ttl)
			entry.ExpiresAt = &exp
		}
		saved, _, err := h.store.UpsertWhitelist(ctx, entry, now)
		if err != nil {
			// The request itself is already approved; only the whitelist part failed.
			h.log.Error("verdict: request approved but the whitelist entry was not saved",
				"request", row.ID, "username", row.Username, "context", row.Context, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		we := model.ToVerdictWhitelistEntry(saved)
		resp.WhitelistEntry = &we
	}

	// Admin denials count toward the auto-block. sudo requests have no source
	// IP and never count (section 8, item 14).
	if row.Status == model.StatusDenied && row.SourceIP != nil && *row.SourceIP != "" {
		block, err := h.rules.RecordDenial(ctx, *row.SourceIP, now)
		if err != nil {
			// The request itself is already denied; only the counting failed.
			h.log.Error("verdict: request denied but the auto-block count failed",
				"request", row.ID, "ip", *row.SourceIP, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if block != nil {
			h.log.Warn("verdict: ip auto-blocked", "request", row.ID, "ip", block.IP, "denials", block.DenialCount)
			resp.AutoBlocked = &model.VerdictAutoBlocked{ID: block.ID, IP: block.IP, DenialCount: block.DenialCount}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// verdictConflict answers 409 with what happened to the request: the winner's
// verdict, or timeout when nobody answered before expires_at. An expired row
// still marked pending is set to timeout here: access-request normally does
// it, but it may run in another process and not have got to it yet.
func (h *Handler) verdictConflict(ctx context.Context, w http.ResponseWriter, id string, now time.Time) {
	row, err := h.store.RequestByID(ctx, id)
	if errNotFound(err) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err != nil {
		h.log.Error("verdict: read request", "request", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if row.Status == model.StatusPending {
		// DecideRequest refused a pending row, so expires_at has passed. Whether
		// this call marks it or another one already has, the outcome is the same.
		if _, err := h.store.MarkTimeout(ctx, id, now); err != nil {
			h.log.Error("verdict: mark timeout", "request", id, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		row.Status = model.StatusTimeout
		row.DecidedAt = nil
		row.DecidedByDeviceLabel = nil
	}
	h.log.Info("verdict: already decided", "request", id, "status", row.Status, "device", deref(row.DecidedByDeviceLabel))
	writeJSON(w, http.StatusConflict, model.VerdictConflict{
		Error:           "request already decided",
		Status:          row.Status,
		DecidedAt:       row.DecidedAt,
		DecidedByDevice: row.DecidedByDeviceLabel,
	})
}
