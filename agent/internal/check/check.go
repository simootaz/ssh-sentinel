// Package check implements the PAM hook: the decision table of docs/architecture.md,
// section 3.1. Run reads the two local lists, asks the backend, and turns the answer into the
// exit code pam_exec expects. Every call logs exactly one audit line.
package check

import (
	"context"
	"os"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
	"github.com/simootaz/ssh-sentinel/agent/internal/whitelist"
)

// Exit codes returned to pam_exec. Anything but 0 denies the login.
const (
	ExitAllow = 0
	ExitDeny  = 1
)

// notifyTimeout caps the best-effort call in notify mode, so a server in rollout never waits
// the full budget for a backend that is down.
const notifyTimeout = 10 * time.Second

// Requester is the single backend call check needs. *client.Client satisfies it.
type Requester interface {
	AccessRequest(ctx context.Context, req client.AccessRequest) (*client.AccessResponse, error)
}

// Deps is everything one decision needs. The command fills it from PAM's environment and the
// config file; tests fill it with fakes.
type Deps struct {
	Context        string           // "ssh" or "sudo"
	Mode           string           // config.ModeEnforce or config.ModeNotify
	User           string           // PAM_USER
	RHost          string           // PAM_RHOST, "" when unknown (sudo)
	TTY            string           // PAM_TTY
	Command        *string          // sudo command line, nil when unknown
	Hostname       string           // reported as "hostname" and logged as host=
	BreakGlassPath string           // /etc/ssh-sentinel/breakglass
	CachePath      string           // /var/lib/ssh-sentinel/whitelist.json
	Client         Requester        // nil when the config could not be loaded
	Timeout        time.Duration    // whole budget of the backend call
	Now            func() time.Time // clock used for cache expiry; tests fake it
	Log            auditlog.Logger
}

// Run applies the decision table and returns the exit code. A panic anywhere ends in deny.
func Run(d Deps) (code int) {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = auditlog.WriterLogger{W: os.Stderr}
	}
	if d.Timeout <= 0 {
		d.Timeout = 30 * time.Second
	}

	line := auditlog.Line{Context: d.Context, User: d.User, RHost: d.RHost, Host: d.Hostname}
	logged := false
	finish := func(decision, reason, request string) int {
		line.Decision, line.Reason, line.Request = decision, reason, request
		d.Log.Log(line)
		logged = true
		if decision == auditlog.Allow {
			return ExitAllow
		}
		return ExitDeny
	}
	defer func() {
		if r := recover(); r != nil {
			if !logged {
				finish(auditlog.Deny, "error", "")
			}
			code = ExitDeny
		}
	}()

	if d.User == "" {
		return finish(auditlog.Deny, "error", "")
	}

	// Row 1: the break-glass file, read before anything touches the network. A missing,
	// unreadable or insecure file is an empty list, never a reason to deny.
	breakGlass, _ := whitelist.ReadBreakGlass(d.BreakGlassPath)
	if breakGlass.Contains(d.User) {
		return finish(auditlog.Allow, "breakglass", "")
	}

	req := client.AccessRequest{
		Context:  d.Context,
		Mode:     config.ModeEnforce,
		Username: d.User,
		Hostname: d.Hostname,
		TTY:      d.TTY,
		Command:  d.Command,
	}
	if d.RHost != "" {
		ip := d.RHost
		req.SourceIP = &ip
	}

	// Row 2: notify mode allows whatever happens; the call only feeds the history.
	if d.Mode == config.ModeNotify {
		req.Mode = config.ModeNotify
		if d.Client == nil {
			return finish(auditlog.Allow, "unreachable", "")
		}
		resp, err := d.call(req, min(d.Timeout, notifyTimeout))
		switch {
		case err == nil:
			return finish(auditlog.Allow, "notify", resp.RequestID)
		case client.IsRejected(err):
			return finish(auditlog.Allow, "rejected", "")
		default:
			return finish(auditlog.Allow, "unreachable", "")
		}
	}

	// Rows 3 to 5: the backend decides. Row 5 (4xx) is final: a backend that refuses this
	// server is up, so the cache must not override it.
	if d.Client != nil {
		resp, err := d.call(req, d.Timeout)
		switch {
		case err == nil:
			if resp.Verdict == "approve" {
				return finish(auditlog.Allow, resp.Reason, resp.RequestID)
			}
			return finish(auditlog.Deny, resp.Reason, resp.RequestID)
		case client.IsRejected(err):
			return finish(auditlog.Deny, "rejected", "")
		}
	}

	// Rows 6 and 7: no usable answer (or no config at all). Only a synced, unexpired entry
	// for this user and context lets the login through.
	cache, _ := whitelist.LoadCache(d.CachePath)
	if cache.Allows(d.User, d.Context, d.Now()) {
		return finish(auditlog.Allow, "cache", "")
	}
	return finish(auditlog.Deny, "unreachable", "")
}

func (d Deps) call(req client.AccessRequest, timeout time.Duration) (*client.AccessResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.Client.AccessRequest(ctx, req)
}
