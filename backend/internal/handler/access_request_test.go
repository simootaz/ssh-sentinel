package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// Tests of POST /access-request. The fake clock makes the 25 s verdict wait
// run in no time: every Sleep advances the clock and calls OnSleep, which is
// where a test decides the request "while" the handler is waiting.

// accessSSHBody is the SSH login example of the contract.
func accessSSHBody() model.AccessRequestBody {
	return model.AccessRequestBody{
		Context:  model.ContextSSH,
		Mode:     model.ModeEnforce,
		Username: "deploy",
		SourceIP: strp("203.0.113.42"),
		Hostname: "web-01",
		TTY:      strp("ssh"),
	}
}

// accessSudoBody is the sudo example of the contract.
func accessSudoBody() model.AccessRequestBody {
	return model.AccessRequestBody{
		Context:  model.ContextSudo,
		Mode:     model.ModeEnforce,
		Username: "deploy",
		SourceIP: nil,
		Hostname: "web-01.internal",
		TTY:      strp("pts/0"),
		Command:  strp("sudo systemctl restart nginx"),
	}
}

// accessPendingID returns the id of the single pending request in the store.
// It runs on the handler's goroutine (from OnSleep), so it reports with
// Errorf rather than Fatalf.
func accessPendingID(e *env) (string, bool) {
	rows, err := e.store.ListRequests(context.Background(), db.RequestFilter{Status: model.StatusPending})
	if err != nil || len(rows) != 1 {
		e.t.Errorf("pending requests: %d rows, err %v", len(rows), err)
		return "", false
	}
	return rows[0].ID, true
}

// accessDecideOnSleep decides the pending request on the n-th sleep of the
// verdict wait, as if a phone had answered at that instant.
func accessDecideOnSleep(e *env, n int, status string, deviceID *string) {
	calls := 0
	e.clock.OnSleep = func(now time.Time) {
		calls++
		if calls != n {
			return
		}
		id, ok := accessPendingID(e)
		if !ok {
			return
		}
		if _, err := e.store.DecideRequest(context.Background(), id, status, deviceID, now); err != nil {
			e.t.Errorf("decide request: %v", err)
		}
	}
}

// accessRow fetches one request row.
func accessRow(e *env, id string) *model.Request {
	e.t.Helper()
	r, err := e.store.RequestByID(context.Background(), id)
	if err != nil {
		e.t.Fatalf("request %s: %v", id, err)
	}
	return r
}

// accessRows returns every request row, newest first.
func accessRows(e *env) []model.Request {
	e.t.Helper()
	rows, err := e.store.ListRequests(context.Background(), db.RequestFilter{})
	if err != nil {
		e.t.Fatalf("list requests: %v", err)
	}
	return rows
}

// accessWhitelist inserts a whitelist entry. serverID nil means every server,
// expires nil means permanent.
func accessWhitelist(e *env, username, reqContext string, serverID *string, expires *time.Time) {
	e.t.Helper()
	entry := &model.WhitelistEntry{Username: username, Context: reqContext, ServerID: serverID, ExpiresAt: expires}
	if _, _, err := e.store.UpsertWhitelist(context.Background(), entry, e.clock.Now()); err != nil {
		e.t.Fatalf("whitelist: %v", err)
	}
}

// accessBlock inserts a blocked_ips row created a minute ago.
func accessBlock(e *env, ip string, expires *time.Time) *model.BlockedIP {
	e.t.Helper()
	b, err := e.store.UpsertBlockedIP(context.Background(), &model.BlockedIP{
		IP:            ip,
		Reason:        "autoblock",
		DenialCount:   3,
		FirstDeniedAt: baseTime.Add(-30 * time.Minute),
		LastDeniedAt:  baseTime.Add(-time.Minute),
		CreatedAt:     baseTime.Add(-time.Minute),
		ExpiresAt:     expires,
	})
	if err != nil {
		e.t.Fatalf("block ip: %v", err)
	}
	return b
}

// accessGeoRule inserts a geo rule for country.
func accessGeoRule(e *env, country string) {
	e.t.Helper()
	if _, err := e.store.CreateGeoRule(context.Background(), country, nil, e.clock.Now()); err != nil {
		e.t.Fatalf("geo rule: %v", err)
	}
}

// accessPost sends the body with the web-01 token and expects a 200.
func accessPost(e *env, body any) model.AccessRequestResponse {
	e.t.Helper()
	st, raw := e.agent(http.MethodPost, "/access-request", body)
	mustStatus(e.t, st, http.StatusOK, raw)
	return decode[model.AccessRequestResponse](e.t, raw)
}

// accessWantVerdict checks the verdict and reason of an answer.
func accessWantVerdict(t *testing.T, resp model.AccessRequestResponse, verdict, reason string) {
	t.Helper()
	if resp.Verdict != verdict || resp.Reason != reason {
		t.Fatalf("verdict %s/%s, want %s/%s", resp.Verdict, resp.Reason, verdict, reason)
	}
	if !isUUID(resp.RequestID) {
		t.Fatalf("request_id %q is not a UUID", resp.RequestID)
	}
}

// accessWantRow checks the status and decided_by of a row.
func accessWantRow(t *testing.T, row *model.Request, status string, decidedBy *string) {
	t.Helper()
	if row.Status != status {
		t.Fatalf("row status %s, want %s", row.Status, status)
	}
	switch {
	case decidedBy == nil && row.DecidedBy != nil:
		t.Fatalf("row decided_by %q, want null", *row.DecidedBy)
	case decidedBy != nil && (row.DecidedBy == nil || *row.DecidedBy != *decidedBy):
		t.Fatalf("row decided_by %v, want %s", row.DecidedBy, *decidedBy)
	}
}

// accessWantTime checks a nullable time against an expected instant.
func accessWantTime(t *testing.T, what string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil || !got.Equal(want) {
		t.Fatalf("%s %v, want %v", what, got, want)
	}
}

// accessWantPush checks that a push carries exactly the keys and values of
// the contract.
func accessWantPush(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("push has %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			t.Fatalf("push lacks key %q: %v", k, got)
		}
		if g != v {
			t.Fatalf("push %s = %q, want %q", k, g, v)
		}
	}
}

// accessWantTimeout checks the whole timeout path: deny/timeout with a null
// decided_at, the row marked timeout, and the wait that lasted VerdictWait.
func accessWantTimeout(e *env, raw []byte, wait time.Duration) *model.Request {
	e.t.Helper()
	resp := decode[model.AccessRequestResponse](e.t, raw)
	accessWantVerdict(e.t, resp, model.VerdictDeny, model.ReasonTimeout)
	m := decodeMap(e.t, raw)
	v, present := m["decided_at"]
	if !present || v != nil {
		e.t.Fatalf("decided_at present=%v value=%v, want present and null", present, v)
	}
	row := accessRow(e, resp.RequestID)
	accessWantRow(e.t, row, model.StatusTimeout, strp(model.DecidedByTimeout))
	if row.DecidedAt != nil {
		e.t.Fatalf("row decided_at %v, want nil", row.DecidedAt)
	}
	if got := e.clock.Now(); !got.Equal(baseTime.Add(wait)) {
		e.t.Fatalf("clock %v, want %v (wait of %v)", got, baseTime.Add(wait), wait)
	}
	return row
}

func TestAccessApprovedAfterPolls(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	e.addDevice("Galaxy S24")
	accessDecideOnSleep(e, 3, model.StatusApproved, &pixel.ID)

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonAdmin)
	accessWantTime(t, "decided_at", resp.DecidedAt, baseTime.Add(3*time.Second))

	row := accessRow(e, resp.RequestID)
	accessWantRow(t, row, model.StatusApproved, strp(model.DecidedByAdmin))
	if row.DecidedByDevice == nil || *row.DecidedByDevice != pixel.ID {
		t.Fatalf("row decided_by_device %v, want %s", row.DecidedByDevice, pixel.ID)
	}
	if got := e.clock.Now(); !got.Equal(baseTime.Add(3 * time.Second)) {
		t.Fatalf("clock %v, want three polls", got)
	}

	pushes := e.push.OfType(model.PushAccessRequest)
	if len(pushes) != 2 || len(e.push.Sent()) != 2 {
		t.Fatalf("%d access_request pushes of %d, want 2 of 2", len(pushes), len(e.push.Sent()))
	}
	want := map[string]string{
		"type":        "access_request",
		"request_id":  resp.RequestID,
		"context":     "ssh",
		"server":      "web-01",
		"username":    "deploy",
		"source_ip":   "203.0.113.42",
		"geo_country": "",
		"geo_city":    "",
		"command":     "",
		"created_at":  "2026-09-14T20:00:00Z",
		"expires_at":  "2026-09-14T20:00:30Z",
	}
	tokens := map[string]bool{}
	for _, p := range pushes {
		tokens[p.Token] = true
		accessWantPush(t, p.Data, want)
	}
	if !tokens["fcm-token-Pixel 8"] || !tokens["fcm-token-Galaxy S24"] {
		t.Fatalf("pushed to %v, want both phones", tokens)
	}
}

func TestAccessDenied(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	accessDecideOnSleep(e, 2, model.StatusDenied, &pixel.ID)

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictDeny, model.ReasonAdmin)
	accessWantTime(t, "decided_at", resp.DecidedAt, baseTime.Add(2*time.Second))
	accessWantRow(t, accessRow(e, resp.RequestID), model.StatusDenied, strp(model.DecidedByAdmin))
}

func TestAccessTimeout(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")

	st, raw := e.agent(http.MethodPost, "/access-request", accessSSHBody())
	mustStatus(t, st, http.StatusOK, raw)
	row := accessWantTimeout(e, raw, DefaultConfig().VerdictWait)
	if !row.ExpiresAt.Equal(baseTime.Add(30 * time.Second)) {
		t.Fatalf("row expires_at %v, want created + 30 s", row.ExpiresAt)
	}
	if !row.CreatedAt.Equal(baseTime) {
		t.Fatalf("row created_at %v, want %v", row.CreatedAt, baseTime)
	}
	if n := len(e.push.OfType(model.PushAccessRequest)); n != 1 {
		t.Fatalf("%d access_request pushes, want 1", n)
	}
}

func TestAccessTimeoutWaitIsExact(t *testing.T) {
	// The wait window is not a multiple of the poll interval: the last sleep
	// is shortened so the agent is answered at exactly VerdictWait.
	e := newEnvWith(t, envOptions{Config: Config{VerdictWait: 5 * time.Second, PollInterval: 2 * time.Second}})
	e.addDevice("Pixel 8")
	st, raw := e.agent(http.MethodPost, "/access-request", accessSSHBody())
	mustStatus(t, st, http.StatusOK, raw)
	accessWantTimeout(e, raw, 5*time.Second)
}

func TestAccessVerdictRacesDeadline(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	deadline := baseTime.Add(DefaultConfig().VerdictWait)
	e.clock.OnSleep = func(now time.Time) {
		if !now.Equal(deadline) {
			return
		}
		id, ok := accessPendingID(e)
		if !ok {
			return
		}
		if _, err := e.store.DecideRequest(context.Background(), id, model.StatusApproved, &pixel.ID, now); err != nil {
			e.t.Errorf("decide request: %v", err)
		}
	}

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonAdmin)
	accessWantTime(t, "decided_at", resp.DecidedAt, deadline)
	accessWantRow(t, accessRow(e, resp.RequestID), model.StatusApproved, strp(model.DecidedByAdmin))
}

func TestAccessWhitelisted(t *testing.T) {
	cases := []struct {
		name     string
		serverID func(e *env) *string
	}{
		{"global entry", func(e *env) *string { return nil }},
		{"entry for this server", func(e *env) *string { return &e.server.ID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addDevice("Pixel 8")
			accessWhitelist(e, "deploy", model.ContextSSH, tc.serverID(e), nil)

			resp := accessPost(e, accessSSHBody())
			accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonWhitelist)
			accessWantTime(t, "decided_at", resp.DecidedAt, baseTime)
			row := accessRow(e, resp.RequestID)
			accessWantRow(t, row, model.StatusWhitelisted, strp(model.DecidedByWhitelist))
			accessWantTime(t, "row decided_at", row.DecidedAt, baseTime)
			if n := len(e.push.Sent()); n != 0 {
				t.Fatalf("%d pushes, want none", n)
			}
			if !e.clock.Now().Equal(baseTime) {
				t.Fatalf("clock advanced to %v, want no wait", e.clock.Now())
			}
		})
	}
}

func TestAccessWhitelistIgnored(t *testing.T) {
	past := baseTime.Add(-time.Second)
	cases := []struct {
		name  string
		setup func(e *env)
	}{
		{"expired entry", func(e *env) { accessWhitelist(e, "deploy", model.ContextSSH, nil, &past) }},
		{"entry for another server", func(e *env) {
			other, _ := e.enrollServer("db-01", model.OSLinux)
			accessWhitelist(e, "deploy", model.ContextSSH, &other.ID, nil)
		}},
		{"entry for the other context", func(e *env) { accessWhitelist(e, "deploy", model.ContextSudo, nil, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.addDevice("Pixel 8")
			tc.setup(e)

			st, raw := e.agent(http.MethodPost, "/access-request", accessSSHBody())
			mustStatus(t, st, http.StatusOK, raw)
			accessWantTimeout(e, raw, DefaultConfig().VerdictWait)
			if n := len(e.push.OfType(model.PushAccessRequest)); n != 1 {
				t.Fatalf("%d access_request pushes, want 1", n)
			}
		})
	}
}

func TestAccessBlockedIP(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	before := accessBlock(e, "203.0.113.42", nil)
	if before.HitCount != 0 || before.LastHitAt != nil {
		t.Fatalf("fresh block has hits: %+v", before)
	}

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictDeny, model.ReasonBlockedIP)
	accessWantTime(t, "decided_at", resp.DecidedAt, baseTime)
	row := accessRow(e, resp.RequestID)
	accessWantRow(t, row, model.StatusBlockedIP, strp(model.DecidedByAutoblock))
	accessWantTime(t, "row decided_at", row.DecidedAt, baseTime)
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes, want none", n)
	}

	after, err := e.store.BlockedIPByIP(context.Background(), "203.0.113.42")
	if err != nil {
		t.Fatal(err)
	}
	if after.HitCount != 1 {
		t.Fatalf("hit_count %d, want 1", after.HitCount)
	}
	accessWantTime(t, "last_hit_at", after.LastHitAt, baseTime)
}

func TestAccessBlockedIPExpired(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	past := baseTime.Add(-time.Minute)
	accessBlock(e, "203.0.113.42", &past)

	st, raw := e.agent(http.MethodPost, "/access-request", accessSSHBody())
	mustStatus(t, st, http.StatusOK, raw)
	accessWantTimeout(e, raw, DefaultConfig().VerdictWait)
	if n := len(e.push.OfType(model.PushAccessRequest)); n != 1 {
		t.Fatalf("%d access_request pushes, want 1", n)
	}
	after, err := e.store.BlockedIPByIP(context.Background(), "203.0.113.42")
	if err != nil {
		t.Fatal(err)
	}
	if after.HitCount != 0 {
		t.Fatalf("hit_count %d on an expired block, want 0", after.HitCount)
	}
}

func TestAccessBlockedGeo(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	e.geo.Set("203.0.113.42", &model.Geo{Country: "KP", City: "Pyongyang"})
	accessGeoRule(e, "KP")

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictDeny, model.ReasonBlockedGeo)
	accessWantTime(t, "decided_at", resp.DecidedAt, baseTime)
	row := accessRow(e, resp.RequestID)
	accessWantRow(t, row, model.StatusBlockedGeo, strp(model.DecidedByGeoRule))
	if row.Geo == nil || row.Geo.Country != "KP" || row.Geo.City != "Pyongyang" {
		t.Fatalf("row geo %+v, want KP/Pyongyang", row.Geo)
	}
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes, want none", n)
	}
}

func TestAccessGeoUnknownIsPushed(t *testing.T) {
	// Geo rules exist but the lookup knows nothing about this IP: an admin decides.
	e := newEnv(t)
	e.addDevice("Pixel 8")
	accessGeoRule(e, "KP")

	st, raw := e.agent(http.MethodPost, "/access-request", accessSSHBody())
	mustStatus(t, st, http.StatusOK, raw)
	row := accessWantTimeout(e, raw, DefaultConfig().VerdictWait)
	if row.Geo != nil {
		t.Fatalf("row geo %+v, want nil", row.Geo)
	}
	if e.geo.Calls != 1 {
		t.Fatalf("geo lookups %d, want 1", e.geo.Calls)
	}
	pushes := e.push.OfType(model.PushAccessRequest)
	if len(pushes) != 1 {
		t.Fatalf("%d access_request pushes, want 1", len(pushes))
	}
	if pushes[0].Data["geo_country"] != "" || pushes[0].Data["geo_city"] != "" {
		t.Fatalf("push geo %q/%q, want empty", pushes[0].Data["geo_country"], pushes[0].Data["geo_city"])
	}
}

func TestAccessGeoNotConsultedForSudo(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	accessGeoRule(e, "KP")
	e.geo.Set("203.0.113.42", &model.Geo{Country: "KP"})
	accessDecideOnSleep(e, 1, model.StatusApproved, &pixel.ID)

	resp := accessPost(e, accessSudoBody())
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonAdmin)
	if e.geo.Calls != 0 {
		t.Fatalf("geo lookups %d, want 0 without a source ip", e.geo.Calls)
	}
}

func TestAccessGeoInPush(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	e.geo.Set("203.0.113.42", &model.Geo{Country: "FR", City: "Paris", ASN: "AS12876"})
	accessDecideOnSleep(e, 1, model.StatusApproved, &pixel.ID)

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonAdmin)
	row := accessRow(e, resp.RequestID)
	if row.Geo == nil || row.Geo.Country != "FR" || row.Geo.City != "Paris" || row.Geo.ASN != "AS12876" {
		t.Fatalf("row geo %+v, want FR/Paris/AS12876", row.Geo)
	}
	pushes := e.push.OfType(model.PushAccessRequest)
	if len(pushes) != 1 {
		t.Fatalf("%d access_request pushes, want 1", len(pushes))
	}
	if pushes[0].Data["geo_country"] != "FR" || pushes[0].Data["geo_city"] != "Paris" {
		t.Fatalf("push geo %q/%q, want FR/Paris", pushes[0].Data["geo_country"], pushes[0].Data["geo_city"])
	}
}

func TestAccessRulesBeforeWhitelist(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	accessWhitelist(e, "deploy", model.ContextSSH, nil, nil)
	accessBlock(e, "203.0.113.42", nil)

	resp := accessPost(e, accessSSHBody())
	accessWantVerdict(t, resp, model.VerdictDeny, model.ReasonBlockedIP)
	accessWantRow(t, accessRow(e, resp.RequestID), model.StatusBlockedIP, strp(model.DecidedByAutoblock))
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes, want none", n)
	}
}

func TestAccessNotifyMode(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	e.addDevice("Galaxy S24")
	body := accessSSHBody()
	body.Mode = model.ModeNotify

	resp := accessPost(e, body)
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonNotify)
	accessWantTime(t, "decided_at", resp.DecidedAt, baseTime)
	row := accessRow(e, resp.RequestID)
	accessWantRow(t, row, model.StatusNotified, nil)
	accessWantTime(t, "row decided_at", row.DecidedAt, baseTime)
	if !e.clock.Now().Equal(baseTime) {
		t.Fatalf("clock advanced to %v, want no wait", e.clock.Now())
	}

	notices := e.push.OfType(model.PushAccessNotice)
	if len(notices) != 2 || len(e.push.Sent()) != 2 {
		t.Fatalf("%d access_notice pushes of %d, want 2 of 2", len(notices), len(e.push.Sent()))
	}
	accessWantPush(t, notices[0].Data, map[string]string{
		"type":        "access_notice",
		"request_id":  resp.RequestID,
		"context":     "ssh",
		"server":      "web-01",
		"username":    "deploy",
		"source_ip":   "203.0.113.42",
		"geo_country": "",
		"geo_city":    "",
		"command":     "",
		"created_at":  "2026-09-14T20:00:00Z",
		"expires_at":  "2026-09-14T20:00:30Z",
	})
}

func TestAccessSudo(t *testing.T) {
	e := newEnv(t)
	pixel := e.addDevice("Pixel 8")
	accessDecideOnSleep(e, 1, model.StatusApproved, &pixel.ID)

	resp := accessPost(e, accessSudoBody())
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonAdmin)
	row := accessRow(e, resp.RequestID)
	if row.Context != model.ContextSudo || row.SourceIP != nil {
		t.Fatalf("row context %s source_ip %v, want sudo and null", row.Context, row.SourceIP)
	}
	if row.Command == nil || *row.Command != "sudo systemctl restart nginx" || row.TTY == nil || *row.TTY != "pts/0" {
		t.Fatalf("row command %v tty %v", row.Command, row.TTY)
	}
	// hostname is display only; the server identity comes from the token.
	if row.Hostname != "web-01.internal" || row.ServerName != "web-01" {
		t.Fatalf("row hostname %q server %q", row.Hostname, row.ServerName)
	}

	pushes := e.push.OfType(model.PushAccessRequest)
	if len(pushes) != 1 {
		t.Fatalf("%d access_request pushes, want 1", len(pushes))
	}
	accessWantPush(t, pushes[0].Data, map[string]string{
		"type":        "access_request",
		"request_id":  resp.RequestID,
		"context":     "sudo",
		"server":      "web-01",
		"username":    "deploy",
		"source_ip":   "",
		"geo_country": "",
		"geo_city":    "",
		"command":     "sudo systemctl restart nginx",
		"created_at":  "2026-09-14T20:00:00Z",
		"expires_at":  "2026-09-14T20:00:30Z",
	})
}

func TestAccessDefaults(t *testing.T) {
	// No context, no mode, no hostname; tty and command sent as "".
	e := newEnv(t)
	e.addDevice("Pixel 8")
	raw := []byte(`{"username":"deploy","source_ip":"203.0.113.42","tty":"","command":""}`)

	st, body := e.agent(http.MethodPost, "/access-request", raw)
	mustStatus(t, st, http.StatusOK, body)
	// mode defaulted to enforce: the request was pushed and waited for.
	row := accessWantTimeout(e, body, DefaultConfig().VerdictWait)
	if row.Context != model.ContextSSH {
		t.Fatalf("row context %q, want ssh", row.Context)
	}
	if row.Hostname != "web-01" {
		t.Fatalf("row hostname %q, want the server name", row.Hostname)
	}
	if row.TTY != nil || row.Command != nil {
		t.Fatalf("row tty %v command %v, want null", row.TTY, row.Command)
	}
}

func TestAccessSourceIPCanonical(t *testing.T) {
	cases := []struct {
		in   string
		want *string
	}{
		{"203.0.113.42", strp("203.0.113.42")},
		{" 203.0.113.42 ", strp("203.0.113.42")},
		{"2001:DB8::0001", strp("2001:db8::1")},
		{"::ffff:203.0.113.42", strp("203.0.113.42")},
		{"", nil},
	}
	e := newEnv(t)
	for _, tc := range cases {
		body := accessSSHBody()
		body.Mode = model.ModeNotify // answered at once, no wait
		body.SourceIP = strp(tc.in)
		resp := accessPost(e, body)
		row := accessRow(e, resp.RequestID)
		switch {
		case tc.want == nil && row.SourceIP != nil:
			t.Fatalf("%q: stored %q, want null", tc.in, *row.SourceIP)
		case tc.want != nil && (row.SourceIP == nil || *row.SourceIP != *tc.want):
			t.Fatalf("%q: stored %v, want %q", tc.in, row.SourceIP, *tc.want)
		}
	}
}

func TestAccessBadRequests(t *testing.T) {
	badContext := accessSSHBody()
	badContext.Context = "root"
	badMode := accessSSHBody()
	badMode.Mode = "maybe"
	blankUser := accessSSHBody()
	blankUser.Username = "   "
	badIP := accessSSHBody()
	badIP.SourceIP = strp("not-an-ip")

	cases := []struct {
		name string
		body any
	}{
		{"invalid json", []byte(`{"username":`)},
		{"json array", []byte(`[]`)},
		{"bad context", badContext},
		{"bad mode", badMode},
		{"missing username", []byte(`{"context":"ssh","source_ip":"203.0.113.42"}`)},
		{"blank username", blankUser},
		{"bad source ip", badIP},
	}
	e := newEnv(t)
	e.addDevice("Pixel 8")
	for _, tc := range cases {
		st, body := e.agent(http.MethodPost, "/access-request", tc.body)
		if st != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400; body %s", tc.name, st, body)
		}
		if decode[model.ErrorResponse](t, body).Error == "" {
			t.Fatalf("%s: empty error message: %s", tc.name, body)
		}
	}
	if n := len(accessRows(e)); n != 0 {
		t.Fatalf("%d rows stored for rejected bodies, want 0", n)
	}
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes for rejected bodies, want 0", n)
	}
	if !e.clock.Now().Equal(baseTime) {
		t.Fatalf("clock advanced to %v, want no wait", e.clock.Now())
	}
}

func TestAccessUnknownFieldsIgnored(t *testing.T) {
	e := newEnv(t)
	e.addDevice("Pixel 8")
	raw := []byte(`{"context":"ssh","mode":"notify","username":"deploy","source_ip":"203.0.113.42","hostname":"web-01","future_field":42}`)

	st, body := e.agent(http.MethodPost, "/access-request", raw)
	mustStatus(t, st, http.StatusOK, body)
	resp := decode[model.AccessRequestResponse](t, body)
	accessWantVerdict(t, resp, model.VerdictApprove, model.ReasonNotify)
}

func TestAccessTouchesServer(t *testing.T) {
	e := newEnv(t)
	if e.server.LastSeenAt != nil {
		t.Fatalf("fresh server already seen at %v", e.server.LastSeenAt)
	}
	body := accessSSHBody()
	body.Mode = model.ModeNotify
	accessPost(e, body)

	srv, err := e.store.ServerByName(context.Background(), "web-01")
	if err != nil {
		t.Fatal(err)
	}
	accessWantTime(t, "last_seen_at", srv.LastSeenAt, baseTime)
}

func TestAccessPushFailureStillWaits(t *testing.T) {
	// FCM down: the request is still pending and times out normally
	// (docs/architecture.md, failure mode 14).
	e := newEnv(t)
	e.addDevice("Pixel 8")
	e.push.Err = errors.New("boom")

	st, raw := e.agent(http.MethodPost, "/access-request", accessSSHBody())
	mustStatus(t, st, http.StatusOK, raw)
	accessWantTimeout(e, raw, DefaultConfig().VerdictWait)
	if n := len(e.push.Sent()); n != 0 {
		t.Fatalf("%d pushes recorded although FCM failed", n)
	}
}
