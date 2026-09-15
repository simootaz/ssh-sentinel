package handler

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
	"github.com/simootaz/ssh-sentinel/backend/internal/rules"
)

// accessRequest serves POST /access-request (docs/architecture.md, section 2
// steps 2 to 11, section 5). The middleware already authenticated the agent;
// the server identity comes from its token, never from the body.
//
// Flow: validate the body, look up the geo of the source IP, run the rules
// (blocked IP, geo rule, whitelist, notify mode), store the row. A request a
// rule decided is answered at once. A pending request is pushed to every
// phone, then the row is re-read until a verdict lands or the wait window
// closes, in which case the row is marked timeout and the agent is told to
// deny.
func (h *Handler) accessRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := auth.PrincipalFrom(ctx)
	if p == nil || p.Server == nil {
		h.log.Error("access-request: no server principal in context")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	srv := p.Server

	var body model.AccessRequestBody
	if !decodeJSON(w, r, &body) {
		return
	}
	if msg := normalizeAccessRequest(&body, srv.Name); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	now := h.clock.Now()

	// Geo is best effort and needs a source IP; sudo has none.
	var geo *model.Geo
	if body.SourceIP != nil && h.geo != nil {
		geo = h.geo.Lookup(ctx, *body.SourceIP)
	}
	country := ""
	if geo != nil {
		country = geo.Country
	}

	outcome, err := h.rules.Evaluate(ctx, rules.Input{
		ServerID: srv.ID,
		Username: body.Username,
		Context:  body.Context,
		SourceIP: body.SourceIP,
		Country:  country,
		Mode:     body.Mode,
	})
	if err != nil {
		h.log.Error("access-request: rules", "server", srv.Name, "username", body.Username, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	row := &model.Request{
		ServerID:  srv.ID,
		Context:   body.Context,
		Username:  body.Username,
		SourceIP:  body.SourceIP,
		Hostname:  body.Hostname,
		TTY:       body.TTY,
		Command:   body.Command,
		Geo:       geo,
		Status:    outcome.Status,
		DecidedBy: outcome.DecidedBy,
		CreatedAt: now,
		ExpiresAt: now.Add(h.cfg.RequestTTL),
	}
	if !outcome.Pending() {
		// A rule decided it on the spot. Pending rows get decided_at from the verdict.
		row.DecidedAt = &now
	}
	created, err := h.store.CreateRequest(ctx, row)
	if err != nil {
		h.log.Error("access-request: insert", "server", srv.Name, "username", body.Username, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if !outcome.Pending() {
		if created.Status == model.StatusNotified {
			// Notify mode: the phones are told, nobody is asked.
			h.pushAll(ctx, created.ID, model.AccessPush(model.PushAccessNotice, created))
		}
		h.answerAccessRequest(w, created)
		return
	}

	h.pushAll(ctx, created.ID, model.AccessPush(model.PushAccessRequest, created))

	// The deadline is measured from the creation instant, so a slow round of
	// pushes eats into the wait instead of stretching the agent's 30 s budget.
	h.waitAccessVerdict(ctx, w, created, now.Add(h.cfg.VerdictWait))
}

// waitAccessVerdict re-reads the row until it is decided or the deadline
// passes. The verdict arrives through the database: the phone's POST /verdict
// may be served by another process (docs/architecture.md, section 3.2).
func (h *Handler) waitAccessVerdict(ctx context.Context, w http.ResponseWriter, created *model.Request, deadline time.Time) {
	for {
		// Never sleep past the deadline, so the wait is exactly VerdictWait
		// whatever the poll interval is.
		d := h.cfg.PollInterval
		if rem := deadline.Sub(h.clock.Now()); rem < d {
			d = rem
		}
		if err := h.clock.Sleep(ctx, d); err != nil {
			// The agent hung up: nobody is listening for the answer any more.
			h.log.Warn("access-request: agent went away", "request", created.ID, "server", created.ServerName, "err", err)
			return
		}
		if !h.clock.Now().Before(deadline) {
			// Last poll: MarkTimeout below is conditional on the row still
			// being pending, so it doubles as the final read.
			break
		}
		cur, err := h.store.RequestByID(ctx, created.ID)
		if err != nil {
			h.log.Error("access-request: re-read", "request", created.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if cur.Status != model.StatusPending {
			h.answerAccessRequest(w, cur)
			return
		}
	}

	marked, err := h.store.MarkTimeout(ctx, created.ID, h.clock.Now())
	if err != nil {
		h.log.Error("access-request: mark timeout", "request", created.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !marked {
		// A verdict landed between the last poll and the deadline: use it.
		cur, err := h.store.RequestByID(ctx, created.ID)
		if err != nil {
			h.log.Error("access-request: re-read after race", "request", created.ID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		h.answerAccessRequest(w, cur)
		return
	}

	// Nobody answered: deny, with decided_at null as the contract says.
	timedOut := *created
	timedOut.Status = model.StatusTimeout
	timedOut.DecidedAt = nil
	h.answerAccessRequest(w, &timedOut)
}

// answerAccessRequest logs the decision and sends the 200 answer for a row
// that is no longer pending.
func (h *Handler) answerAccessRequest(w http.ResponseWriter, row *model.Request) {
	verdict, reason := model.VerdictFor(row.Status)
	h.log.Info("access-request decided",
		"request", row.ID, "server", row.ServerName, "context", row.Context,
		"username", row.Username, "status", row.Status, "verdict", verdict, "reason", reason)
	writeJSON(w, http.StatusOK, model.AccessRequestResponse{
		RequestID: row.ID,
		Verdict:   verdict,
		Reason:    reason,
		DecidedAt: row.DecidedAt,
	})
}

// normalizeAccessRequest applies the defaults of the contract and checks the
// enumerations, editing the body in place. It returns the message of the 400
// answer, or "" when the body is acceptable. The server name only fills the
// display hostname when the agent sent none.
func normalizeAccessRequest(b *model.AccessRequestBody, serverName string) string {
	if b.Context == "" {
		b.Context = model.ContextSSH
	}
	if !model.ValidContext(b.Context) {
		return "context must be ssh or sudo"
	}
	if b.Mode == "" {
		b.Mode = model.ModeEnforce
	}
	if !model.ValidMode(b.Mode) {
		return "mode must be enforce or notify"
	}
	b.Username = strings.TrimSpace(b.Username)
	if b.Username == "" {
		return "username is required"
	}
	b.Hostname = strings.TrimSpace(b.Hostname)
	if b.Hostname == "" {
		b.Hostname = serverName
	}

	// null and "" both mean unknown (sudo has no remote host). Anything else
	// must be an IP address, stored in its canonical spelling so that it
	// compares equal to the blocked_ips rows and to what the app shows.
	if b.SourceIP != nil {
		raw := strings.TrimSpace(*b.SourceIP)
		if raw == "" {
			b.SourceIP = nil
		} else {
			addr, err := netip.ParseAddr(raw)
			if err != nil {
				return "source_ip must be an IP address"
			}
			// Unmap turns ::ffff:203.0.113.42 into 203.0.113.42; WithZone drops
			// an interface suffix (fe80::1%eth0) that INET does not accept.
			canon := addr.Unmap().WithZone("").String()
			b.SourceIP = &canon
		}
	}

	b.TTY = accessNilIfEmpty(b.TTY)
	b.Command = accessNilIfEmpty(b.Command)
	return ""
}

// accessNilIfEmpty maps "" to nil: the column is nullable and "" says nothing.
func accessNilIfEmpty(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}
