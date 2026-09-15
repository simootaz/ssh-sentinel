package handler

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
	"github.com/simootaz/ssh-sentinel/backend/internal/rules"
)

// Tests of POST /verdict. Pending rows are inserted straight into the store,
// so these tests do not depend on the access-request route.

const verdictIP = "203.0.113.42"

// verdictRequest inserts a request row. Zero fields get the defaults of an
// ssh login of deploy on web-01: pending, created now, expiring 30 s later.
func verdictRequest(e *env, r model.Request) *model.Request {
	e.t.Helper()
	if r.ServerID == "" {
		r.ServerID = e.server.ID
	}
	if r.Context == "" {
		r.Context = model.ContextSSH
	}
	if r.Username == "" {
		r.Username = "deploy"
	}
	if r.Hostname == "" {
		r.Hostname = "web-01"
	}
	if r.Status == "" {
		r.Status = model.StatusPending
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = e.clock.Now()
	}
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = r.CreatedAt.Add(30 * time.Second)
	}
	row, err := e.store.CreateRequest(context.Background(), &r)
	if err != nil {
		e.t.Fatalf("create request: %v", err)
	}
	return row
}

// verdictSSH inserts a pending ssh request from the usual IP.
func verdictSSH(e *env) *model.Request {
	e.t.Helper()
	return verdictRequest(e, model.Request{SourceIP: strp(verdictIP), TTY: strp("ssh")})
}

// verdictSudo inserts a pending sudo request: no source IP, a command.
func verdictSudo(e *env) *model.Request {
	e.t.Helper()
	return verdictRequest(e, model.Request{Context: model.ContextSudo, TTY: strp("pts/0"), Command: strp("sudo systemctl restart nginx")})
}

// verdictBody builds the POST /verdict payload. dev nil sends no device_id.
func verdictBody(id, verdict string, dev *model.Device) model.VerdictBody {
	b := model.VerdictBody{RequestID: id, Verdict: verdict}
	if dev != nil {
		b.DeviceID = &dev.ID
	}
	return b
}

// verdictWithTTL adds ttl_seconds to a payload.
func verdictWithTTL(b model.VerdictBody, ttl int64) model.VerdictBody {
	b.TTLSeconds = &ttl
	return b
}

// verdictPost sends the verdict with the admin token and expects 200.
func verdictPost(e *env, body model.VerdictBody) model.VerdictResponse {
	e.t.Helper()
	st, out := e.admin(http.MethodPost, "/verdict", body)
	mustStatus(e.t, st, http.StatusOK, out)
	return decode[model.VerdictResponse](e.t, out)
}

// verdictDenyFrom inserts a pending ssh request from ip and denies it.
func verdictDenyFrom(e *env, ip string, dev *model.Device) model.VerdictResponse {
	e.t.Helper()
	req := verdictRequest(e, model.Request{SourceIP: strp(ip)})
	return verdictPost(e, verdictBody(req.ID, model.VerdictDeny, dev))
}

// verdictRow re-reads a request from the store.
func verdictRow(e *env, id string) *model.Request {
	e.t.Helper()
	row, err := e.store.RequestByID(context.Background(), id)
	if err != nil {
		e.t.Fatalf("request %s: %v", id, err)
	}
	return row
}

// verdictBlock reads the blocked_ips row of ip, failing when there is none.
func verdictBlock(e *env, ip string) *model.BlockedIP {
	e.t.Helper()
	b, err := e.store.BlockedIPByIP(context.Background(), ip)
	if err != nil {
		e.t.Fatalf("blocked ip %s: %v", ip, err)
	}
	return b
}

// verdictNoBlock fails when ip has a blocked_ips row.
func verdictNoBlock(e *env, ip string) {
	e.t.Helper()
	b, err := e.store.BlockedIPByIP(context.Background(), ip)
	if !errors.Is(err, db.ErrNotFound) {
		e.t.Fatalf("blocked ip %s: got %+v, %v; want not found", ip, b, err)
	}
}

// verdictWhitelisted reports whether an unexpired entry matches, on any server.
func verdictWhitelisted(e *env, username, reqContext, serverID string) (*model.WhitelistEntry, bool) {
	e.t.Helper()
	w, err := e.store.FindWhitelist(context.Background(), username, reqContext, serverID, e.clock.Now())
	if errors.Is(err, db.ErrNotFound) {
		return nil, false
	}
	if err != nil {
		e.t.Fatalf("find whitelist: %v", err)
	}
	return w, true
}

// verdictSameTime fails when got is nil or differs from want.
func verdictSameTime(t *testing.T, name string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil || !got.Equal(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

// verdictNullKey fails when m lacks key or its value is not null.
func verdictNullKey(t *testing.T, m map[string]any, key string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("%s must be present and null, got absent: %v", key, m)
	}
	if v != nil {
		t.Fatalf("%s must be null, got %v", key, v)
	}
}

func TestVerdictApprove(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	e.addDevice("Galaxy S24")
	req := verdictSSH(e)

	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictApprove, pixel))
	mustStatus(t, st, http.StatusOK, body)

	resp := decode[model.VerdictResponse](t, body)
	if resp.RequestID != req.ID || resp.Status != model.StatusApproved {
		t.Fatalf("body %s", body)
	}
	verdictSameTime(t, "decided_at", resp.DecidedAt, baseTime)
	if deref(resp.DecidedByDevice) != "Pixel 8" {
		t.Fatalf("decided_by_device %v, want Pixel 8", resp.DecidedByDevice)
	}

	m := decodeMap(t, body)
	if _, ok := m["whitelist_entry"]; ok {
		t.Fatalf("whitelist_entry must be absent for approve: %s", body)
	}
	verdictNullKey(t, m, "auto_blocked")

	row := verdictRow(e, req.ID)
	if row.Status != model.StatusApproved || deref(row.DecidedBy) != model.DecidedByAdmin || deref(row.DecidedByDevice) != pixel.ID {
		t.Fatalf("row %+v", row)
	}
	verdictSameTime(t, "row decided_at", row.DecidedAt, baseTime)

	// Every phone gets request_decided, the deciding one included.
	pushes := e.push.OfType(model.PushRequestDecided)
	if len(pushes) != 2 || len(e.push.Sent()) != 2 {
		t.Fatalf("%d request_decided pushes of %d sent, want 2 of 2", len(pushes), len(e.push.Sent()))
	}
	want := map[string]string{
		"type":              model.PushRequestDecided,
		"request_id":        req.ID,
		"status":            model.StatusApproved,
		"decided_by_device": "Pixel 8",
	}
	tokens := map[string]bool{}
	for _, p := range pushes {
		tokens[p.Token] = true
		if !maps.Equal(p.Data, want) {
			t.Fatalf("push data %v, want %v", p.Data, want)
		}
	}
	if !tokens["fcm-token-Pixel 8"] || !tokens["fcm-token-Galaxy S24"] {
		t.Fatalf("pushed to %v, want both phones", tokens)
	}
}

func TestVerdictDeny(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	req := verdictSSH(e)

	resp := verdictPost(e, verdictBody(req.ID, model.VerdictDeny, pixel))
	if resp.RequestID != req.ID || resp.Status != model.StatusDenied {
		t.Fatalf("resp %+v", resp)
	}
	verdictSameTime(t, "decided_at", resp.DecidedAt, baseTime)
	if deref(resp.DecidedByDevice) != "Pixel 8" || resp.WhitelistEntry != nil || resp.AutoBlocked != nil {
		t.Fatalf("resp %+v", resp)
	}

	row := verdictRow(e, req.ID)
	if row.Status != model.StatusDenied || deref(row.DecidedBy) != model.DecidedByAdmin || deref(row.DecidedByDevice) != pixel.ID {
		t.Fatalf("row %+v", row)
	}
	verdictNoBlock(e, verdictIP)

	pushes := e.push.OfType(model.PushRequestDecided)
	if len(pushes) != 1 || pushes[0].Data["status"] != model.StatusDenied || pushes[0].Data["decided_by_device"] != "Pixel 8" {
		t.Fatalf("pushes %+v", pushes)
	}
}

func TestVerdictUpperCaseIDsAccepted(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	req := verdictSSH(e)

	body := model.VerdictBody{
		RequestID: strings.ToUpper(req.ID),
		Verdict:   model.VerdictApprove,
		DeviceID:  strp(strings.ToUpper(pixel.ID)),
	}
	resp := verdictPost(e, body)
	if resp.RequestID != req.ID || deref(resp.DecidedByDevice) != "Pixel 8" {
		t.Fatalf("resp %+v", resp)
	}
	if row := verdictRow(e, req.ID); deref(row.DecidedByDevice) != pixel.ID {
		t.Fatalf("row %+v", row)
	}
}

func TestVerdictApproveAlwaysWithTTL(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	other, _ := e.enrollServer("web-02", model.OSLinux)
	req := verdictSSH(e)

	resp := verdictPost(e, verdictWithTTL(verdictBody(req.ID, model.VerdictApproveAlways, pixel), 3600))
	if resp.Status != model.StatusApproved || resp.AutoBlocked != nil {
		t.Fatalf("resp %+v", resp)
	}
	we := resp.WhitelistEntry
	if we == nil {
		t.Fatalf("no whitelist_entry: %+v", resp)
	}
	if !isUUID(we.ID) || we.Username != "deploy" || we.Context != model.ContextSSH || deref(we.Server) != "web-01" {
		t.Fatalf("whitelist_entry %+v", we)
	}
	verdictSameTime(t, "whitelist_entry.expires_at", we.ExpiresAt, baseTime.Add(time.Hour))

	found, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, e.server.ID)
	if !ok {
		t.Fatalf("entry not found for web-01")
	}
	if found.ID != we.ID || deref(found.CreatedFrom) != req.ID || deref(found.CreatedByDevice) != pixel.ID {
		t.Fatalf("stored entry %+v, want id %s from request %s by device %s", found, we.ID, req.ID, pixel.ID)
	}
	verdictSameTime(t, "stored expires_at", found.ExpiresAt, baseTime.Add(time.Hour))
	if _, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, other.ID); ok {
		t.Fatalf("entry must be for web-01 only, found for web-02")
	}

	if row := verdictRow(e, req.ID); row.Status != model.StatusApproved || deref(row.DecidedByDevice) != pixel.ID {
		t.Fatalf("row %+v", row)
	}
	if pushes := e.push.OfType(model.PushRequestDecided); len(pushes) != 1 || pushes[0].Data["status"] != model.StatusApproved {
		t.Fatalf("pushes %+v", pushes)
	}
}

func TestVerdictApproveAlwaysPermanent(t *testing.T) {
	e := newEnv(t)
	req := verdictSSH(e)

	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictApproveAlways, nil))
	mustStatus(t, st, http.StatusOK, body)
	m := decodeMap(t, body)
	we, ok := m["whitelist_entry"].(map[string]any)
	if !ok {
		t.Fatalf("whitelist_entry missing or not an object: %s", body)
	}
	verdictNullKey(t, we, "expires_at")
	if we["server"] != "web-01" || we["username"] != "deploy" || we["context"] != model.ContextSSH {
		t.Fatalf("whitelist_entry %v", we)
	}

	found, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, e.server.ID)
	if !ok || found.ExpiresAt != nil || found.CreatedByDevice != nil || deref(found.CreatedFrom) != req.ID {
		t.Fatalf("stored entry %+v", found)
	}
	// A permanent entry is still there long after.
	e.clock.Advance(24 * 365 * time.Hour)
	if _, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, e.server.ID); !ok {
		t.Fatalf("permanent entry expired")
	}
}

func TestVerdictApproveAlwaysSudoOnly(t *testing.T) {
	e := newEnv(t)
	req := verdictSudo(e)

	resp := verdictPost(e, verdictBody(req.ID, model.VerdictApproveAlways, nil))
	if resp.WhitelistEntry == nil || resp.WhitelistEntry.Context != model.ContextSudo {
		t.Fatalf("resp %+v", resp)
	}
	if _, ok := verdictWhitelisted(e, "deploy", model.ContextSudo, e.server.ID); !ok {
		t.Fatalf("sudo entry not found")
	}
	if _, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, e.server.ID); ok {
		t.Fatalf("always-allow on sudo must not whitelist ssh")
	}
}

func TestVerdictApproveAlwaysTwiceRefreshes(t *testing.T) {
	e := newEnv(t)
	first := verdictSSH(e)
	r1 := verdictPost(e, verdictWithTTL(verdictBody(first.ID, model.VerdictApproveAlways, nil), 3600))

	e.clock.Advance(30 * time.Minute)
	second := verdictSSH(e)
	r2 := verdictPost(e, verdictWithTTL(verdictBody(second.ID, model.VerdictApproveAlways, nil), 86400))

	if r1.WhitelistEntry == nil || r2.WhitelistEntry == nil {
		t.Fatalf("missing whitelist_entry: %+v / %+v", r1, r2)
	}
	if r2.WhitelistEntry.ID != r1.WhitelistEntry.ID {
		t.Fatalf("second entry id %s, want the first one %s", r2.WhitelistEntry.ID, r1.WhitelistEntry.ID)
	}
	verdictSameTime(t, "refreshed expires_at", r2.WhitelistEntry.ExpiresAt, e.clock.Now().Add(24*time.Hour))

	list, err := e.store.ListWhitelist(context.Background(), nil, e.clock.Now())
	if err != nil || len(list) != 1 {
		t.Fatalf("whitelist has %d entries (%v), want 1", len(list), err)
	}
	verdictSameTime(t, "stored expires_at", list[0].ExpiresAt, e.clock.Now().Add(24*time.Hour))
}

func TestVerdictApproveAlwaysBadTTL(t *testing.T) {
	e := newEnv(t)
	req := verdictSSH(e)
	for _, ttl := range []int64{0, -5} {
		st, body := e.admin(http.MethodPost, "/verdict", verdictWithTTL(verdictBody(req.ID, model.VerdictApproveAlways, nil), ttl))
		mustStatus(t, st, http.StatusBadRequest, body)
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("ttl %d: body %s", ttl, body)
		}
	}
	if row := verdictRow(e, req.ID); row.Status != model.StatusPending {
		t.Fatalf("a rejected verdict must not decide the request: %+v", row)
	}
	if _, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, e.server.ID); ok {
		t.Fatalf("a rejected verdict must not whitelist")
	}
}

func TestVerdictApproveIgnoresTTL(t *testing.T) {
	e := newEnv(t)
	// ttl_seconds is only read with approve_always: a plain approve takes it
	// as is, even a value that approve_always would reject.
	for _, ttl := range []int64{3600, 0} {
		req := verdictSSH(e)
		resp := verdictPost(e, verdictWithTTL(verdictBody(req.ID, model.VerdictApprove, nil), ttl))
		if resp.Status != model.StatusApproved || resp.WhitelistEntry != nil {
			t.Fatalf("ttl %d: resp %+v", ttl, resp)
		}
	}
	if _, ok := verdictWhitelisted(e, "deploy", model.ContextSSH, e.server.ID); ok {
		t.Fatalf("approve must not whitelist")
	}
}

func TestVerdictSecondVerdictConflict(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	galaxy := e.addDevice("Galaxy S24")
	req := verdictSSH(e)

	verdictPost(e, verdictBody(req.ID, model.VerdictApprove, pixel))
	sentBefore := len(e.push.Sent())
	e.clock.Advance(2 * time.Second)

	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictDeny, galaxy))
	mustStatus(t, st, http.StatusConflict, body)
	c := decode[model.VerdictConflict](t, body)
	if c.Error != "request already decided" || c.Status != model.StatusApproved {
		t.Fatalf("body %s", body)
	}
	verdictSameTime(t, "decided_at", c.DecidedAt, baseTime)
	if deref(c.DecidedByDevice) != "Pixel 8" {
		t.Fatalf("decided_by_device %v, want Pixel 8", c.DecidedByDevice)
	}

	if got := len(e.push.Sent()); got != sentBefore {
		t.Fatalf("the losing verdict pushed: %d sent, want %d", got, sentBefore)
	}
	row := verdictRow(e, req.ID)
	if row.Status != model.StatusApproved || deref(row.DecidedByDevice) != pixel.ID {
		t.Fatalf("the losing verdict changed the row: %+v", row)
	}
}

func TestVerdictExpiredRequestIsTimeout(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	req := verdictSSH(e)
	e.clock.Advance(31 * time.Second)

	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictApprove, pixel))
	mustStatus(t, st, http.StatusConflict, body)
	m := decodeMap(t, body)
	if m["error"] != "request already decided" || m["status"] != model.StatusTimeout {
		t.Fatalf("body %s", body)
	}
	verdictNullKey(t, m, "decided_at")
	verdictNullKey(t, m, "decided_by_device")

	row := verdictRow(e, req.ID)
	if row.Status != model.StatusTimeout || deref(row.DecidedBy) != model.DecidedByTimeout || row.DecidedAt != nil || row.DecidedByDevice != nil {
		t.Fatalf("row %+v", row)
	}
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes, want none on a conflict", n)
	}
}

func TestVerdictAlreadyTimeout(t *testing.T) {
	e := newEnv(t)
	req := verdictSSH(e)
	e.clock.Advance(31 * time.Second)
	if ok, err := e.store.MarkTimeout(context.Background(), req.ID, e.clock.Now()); !ok || err != nil {
		t.Fatalf("mark timeout: %v %v", ok, err)
	}

	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictDeny, nil))
	mustStatus(t, st, http.StatusConflict, body)
	m := decodeMap(t, body)
	if m["error"] != "request already decided" || m["status"] != model.StatusTimeout {
		t.Fatalf("body %s", body)
	}
	verdictNullKey(t, m, "decided_at")
	verdictNullKey(t, m, "decided_by_device")
}

func TestVerdictUnknownRequest(t *testing.T) {
	e := newEnv(t)
	st, body := e.admin(http.MethodPost, "/verdict", verdictBody("5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11", model.VerdictApprove, nil))
	mustStatus(t, st, http.StatusNotFound, body)
	if decode[model.ErrorResponse](t, body).Error != "request not found" {
		t.Fatalf("body %s", body)
	}
}

func TestVerdictBadRequests(t *testing.T) {
	e := newEnv(t)
	req := verdictSSH(e)
	cases := []struct {
		name string
		body any
	}{
		{"malformed request id", model.VerdictBody{RequestID: "not-a-uuid", Verdict: model.VerdictApprove}},
		{"missing request id", model.VerdictBody{Verdict: model.VerdictApprove}},
		{"bad verdict", model.VerdictBody{RequestID: req.ID, Verdict: "maybe"}},
		{"missing verdict", model.VerdictBody{RequestID: req.ID}},
		{"invalid JSON", []byte(`{"request_id": "` + req.ID + `", "verdict": `)},
		{"malformed device_id", model.VerdictBody{RequestID: req.ID, Verdict: model.VerdictApprove, DeviceID: strp("phone-1")}},
		{"empty device_id", model.VerdictBody{RequestID: req.ID, Verdict: model.VerdictApprove, DeviceID: strp("")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, body := e.admin(http.MethodPost, "/verdict", c.body)
			mustStatus(t, st, http.StatusBadRequest, body)
			if decode[model.ErrorResponse](t, body).Error == "" {
				t.Fatalf("body %s", body)
			}
		})
	}
	if row := verdictRow(e, req.ID); row.Status != model.StatusPending {
		t.Fatalf("a rejected verdict must not decide the request: %+v", row)
	}
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes, want none", n)
	}
}

func TestVerdictUnknownDevice(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	gone := e.addDevice("Old phone")
	if err := e.store.DeleteDevice(context.Background(), gone.ID, e.clock.Now()); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	req := verdictSSH(e)

	// The phone's row is gone but its id is well-formed: the admin token is
	// what authorizes the verdict, so it goes through without a device.
	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictApprove, gone))
	mustStatus(t, st, http.StatusOK, body)
	m := decodeMap(t, body)
	if m["status"] != model.StatusApproved {
		t.Fatalf("body %s", body)
	}
	verdictNullKey(t, m, "decided_by_device")

	row := verdictRow(e, req.ID)
	if row.Status != model.StatusApproved || row.DecidedByDevice != nil || row.DecidedByDeviceLabel != nil {
		t.Fatalf("row %+v", row)
	}
	if pushes := e.push.OfType(model.PushRequestDecided); len(pushes) != 1 || pushes[0].Data["decided_by_device"] != "" {
		t.Fatalf("pushes %+v", pushes)
	}
}

func TestVerdictNoDevice(t *testing.T) {
	e := newEnv(t)
	req := verdictSSH(e)

	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictApprove, nil))
	mustStatus(t, st, http.StatusOK, body)
	m := decodeMap(t, body)
	if m["status"] != model.StatusApproved || m["request_id"] != req.ID {
		t.Fatalf("body %s", body)
	}
	verdictNullKey(t, m, "decided_by_device")
	if row := verdictRow(e, req.ID); row.DecidedByDevice != nil || deref(row.DecidedBy) != model.DecidedByAdmin {
		t.Fatalf("row %+v", row)
	}
}

func TestVerdictAutoBlockDefault(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")

	first := e.clock.Now()
	for i := 1; i <= 2; i++ {
		if resp := verdictDenyFrom(e, verdictIP, pixel); resp.AutoBlocked != nil {
			t.Fatalf("denial %d auto-blocked: %+v", i, resp.AutoBlocked)
		}
		e.clock.Advance(time.Minute)
	}
	verdictNoBlock(e, verdictIP)

	third := e.clock.Now()
	resp := verdictDenyFrom(e, verdictIP, pixel)
	if resp.AutoBlocked == nil {
		t.Fatalf("third denial did not auto-block: %+v", resp)
	}
	if !isUUID(resp.AutoBlocked.ID) || resp.AutoBlocked.IP != verdictIP || resp.AutoBlocked.DenialCount != 3 {
		t.Fatalf("auto_blocked %+v", resp.AutoBlocked)
	}

	b := verdictBlock(e, verdictIP)
	if b.ID != resp.AutoBlocked.ID || !b.Active(e.clock.Now()) || b.DenialCount != 3 || b.Reason != "autoblock" {
		t.Fatalf("block %+v", b)
	}
	if b.ExpiresAt != nil {
		t.Fatalf("default block must last until unblocked, got expires_at %v", b.ExpiresAt)
	}
	if !b.FirstDeniedAt.Equal(first) || !b.LastDeniedAt.Equal(third) {
		t.Fatalf("first_denied_at %v last_denied_at %v, want %v and %v", b.FirstDeniedAt, b.LastDeniedAt, first, third)
	}
}

func TestVerdictAutoBlockIgnoresTimeouts(t *testing.T) {
	e := newEnv(t)
	verdictDenyFrom(e, verdictIP, nil)
	verdictDenyFrom(e, verdictIP, nil)

	// A third request from the same IP that nobody answered.
	late := verdictSSH(e)
	e.clock.Advance(31 * time.Second)
	st, body := e.admin(http.MethodPost, "/verdict", verdictBody(late.ID, model.VerdictDeny, nil))
	mustStatus(t, st, http.StatusConflict, body)
	if row := verdictRow(e, late.ID); row.Status != model.StatusTimeout {
		t.Fatalf("row %+v", row)
	}
	verdictNoBlock(e, verdictIP)

	// A real third denial inside the window does block.
	if resp := verdictDenyFrom(e, verdictIP, nil); resp.AutoBlocked == nil || resp.AutoBlocked.DenialCount != 3 {
		t.Fatalf("resp %+v", resp)
	}
}

func TestVerdictAutoBlockWindow(t *testing.T) {
	e := newEnv(t)
	verdictDenyFrom(e, verdictIP, nil)
	e.clock.Advance(2 * time.Hour)
	for i := 2; i <= 3; i++ {
		if resp := verdictDenyFrom(e, verdictIP, nil); resp.AutoBlocked != nil {
			t.Fatalf("denial %d auto-blocked although the first one is outside the window: %+v", i, resp.AutoBlocked)
		}
	}
	verdictNoBlock(e, verdictIP)
}

func TestVerdictAutoBlockCustomThreshold(t *testing.T) {
	e := newEnvWith(t, envOptions{Rules: rules.Config{Threshold: 2, Window: time.Hour}})
	if resp := verdictDenyFrom(e, verdictIP, nil); resp.AutoBlocked != nil {
		t.Fatalf("first denial auto-blocked: %+v", resp.AutoBlocked)
	}
	resp := verdictDenyFrom(e, verdictIP, nil)
	if resp.AutoBlocked == nil || resp.AutoBlocked.DenialCount != 2 || resp.AutoBlocked.IP != verdictIP {
		t.Fatalf("resp %+v", resp)
	}
	if b := verdictBlock(e, verdictIP); b.ID != resp.AutoBlocked.ID || b.ExpiresAt != nil {
		t.Fatalf("block %+v", b)
	}
}

func TestVerdictAutoBlockDuration(t *testing.T) {
	e := newEnvWith(t, envOptions{Rules: rules.Config{Threshold: 2, Window: time.Hour, BlockDuration: 10 * time.Minute}})
	verdictDenyFrom(e, verdictIP, nil)
	resp := verdictDenyFrom(e, verdictIP, nil)
	if resp.AutoBlocked == nil {
		t.Fatalf("second denial did not auto-block: %+v", resp)
	}
	b := verdictBlock(e, verdictIP)
	verdictSameTime(t, "block expires_at", b.ExpiresAt, e.clock.Now().Add(10*time.Minute))
	if !b.Active(e.clock.Now()) || b.Active(e.clock.Now().Add(10*time.Minute)) {
		t.Fatalf("block %+v must be active now and over in 10 min", b)
	}
}

func TestVerdictAutoBlockAfterUnblock(t *testing.T) {
	e := newEnv(t)
	var resp model.VerdictResponse
	for i := 1; i <= 3; i++ {
		resp = verdictDenyFrom(e, verdictIP, nil)
		e.clock.Advance(time.Minute)
	}
	if resp.AutoBlocked == nil {
		t.Fatalf("third denial did not auto-block: %+v", resp)
	}
	blockID := resp.AutoBlocked.ID

	if err := e.store.UnblockIP(context.Background(), blockID, e.clock.Now()); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	e.clock.Advance(time.Minute)

	// The three denials before the unblock are still inside the window but
	// must not be counted again: two new ones are not enough.
	for i := 4; i <= 5; i++ {
		if resp := verdictDenyFrom(e, verdictIP, nil); resp.AutoBlocked != nil {
			t.Fatalf("denial %d re-blocked with the old denials: %+v", i, resp.AutoBlocked)
		}
		e.clock.Advance(time.Minute)
	}
	if b := verdictBlock(e, verdictIP); b.Active(e.clock.Now()) {
		t.Fatalf("block re-armed too early: %+v", b)
	}

	resp = verdictDenyFrom(e, verdictIP, nil)
	if resp.AutoBlocked == nil || resp.AutoBlocked.DenialCount != 3 {
		t.Fatalf("sixth denial (third since the unblock) did not re-block: %+v", resp)
	}
	if resp.AutoBlocked.ID != blockID {
		t.Fatalf("re-armed block id %s, want the same row %s", resp.AutoBlocked.ID, blockID)
	}
	if b := verdictBlock(e, verdictIP); !b.Active(e.clock.Now()) || b.DenialCount != 3 {
		t.Fatalf("block %+v", b)
	}
}

func TestVerdictDenyWhileBlocked(t *testing.T) {
	e := newEnv(t)
	// A request from the IP that was already pending when the block landed.
	late := verdictSSH(e)
	for i := 1; i <= 3; i++ {
		verdictDenyFrom(e, verdictIP, nil)
	}
	before := verdictBlock(e, verdictIP)

	resp := verdictPost(e, verdictBody(late.ID, model.VerdictDeny, nil))
	if resp.Status != model.StatusDenied || resp.AutoBlocked != nil {
		t.Fatalf("resp %+v", resp)
	}
	after := verdictBlock(e, verdictIP)
	if after.ID != before.ID || after.DenialCount != before.DenialCount || !after.LastDeniedAt.Equal(before.LastDeniedAt) ||
		!after.CreatedAt.Equal(before.CreatedAt) || after.ExpiresAt != nil {
		t.Fatalf("block changed: before %+v, after %+v", before, after)
	}
	if row := verdictRow(e, late.ID); row.Status != model.StatusDenied {
		t.Fatalf("row %+v", row)
	}
}

func TestVerdictSudoDenyNeverBlocks(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 3; i++ {
		req := verdictSudo(e)
		resp := verdictPost(e, verdictBody(req.ID, model.VerdictDeny, nil))
		if resp.Status != model.StatusDenied || resp.AutoBlocked != nil {
			t.Fatalf("sudo denial %d: %+v", i, resp)
		}
		if row := verdictRow(e, req.ID); row.Status != model.StatusDenied {
			t.Fatalf("row %+v", row)
		}
	}
	list, err := e.store.ListBlockedIPs(context.Background(), e.clock.Now())
	if err != nil || len(list) != 0 {
		t.Fatalf("blocked ips %+v (%v), want none", list, err)
	}
}

func TestVerdictServerTokenForbidden(t *testing.T) {
	e := newEnv(t)
	req := verdictSSH(e)
	st, body := e.agent(http.MethodPost, "/verdict", verdictBody(req.ID, model.VerdictApprove, nil))
	mustStatus(t, st, http.StatusForbidden, body)
	if row := verdictRow(e, req.ID); row.Status != model.StatusPending {
		t.Fatalf("a server token decided a request: %+v", row)
	}
}
