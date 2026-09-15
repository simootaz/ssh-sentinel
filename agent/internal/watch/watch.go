package watch

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
)

// Event is one accepted SSH login read from the event log.
type Event struct {
	RecordID uint64
	Time     time.Time
	Username string
	SourceIP string
	Method   string
}

// Source delivers new events on each call. The Windows implementation polls wevtutil; tests
// use a scripted one.
type Source interface {
	Next(ctx context.Context) ([]Event, error)
}

// Requester is the single backend call the watcher needs. *client.Client satisfies it.
type Requester interface {
	AccessRequest(ctx context.Context, req client.AccessRequest) (*client.AccessResponse, error)
}

// Watcher reports every accepted login to the backend in notify mode and logs the outcome.
// It never blocks a login: the session is already open when the event is read.
type Watcher struct {
	Source   Source
	Client   Requester
	Hostname string
	Log      auditlog.Logger
	Poll     time.Duration // default 2 s
	Timeout  time.Duration // per request, default 10 s
	Errors   io.Writer     // where Source errors go, default io.Discard
}

// RunOnce reads the source once and reports every event. It returns the number reported.
func (w *Watcher) RunOnce(ctx context.Context) (int, error) {
	events, err := w.Source.Next(ctx)
	if err != nil {
		return 0, err
	}
	for _, ev := range events {
		w.report(ctx, ev)
	}
	return len(events), nil
}

// Run polls until ctx is cancelled. Source errors are printed and polling continues, because
// a transient wevtutil failure must not stop the watcher.
func (w *Watcher) Run(ctx context.Context) error {
	poll := w.Poll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	errors := w.Errors
	if errors == nil {
		errors = io.Discard
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintf(errors, "watch: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Watcher) report(ctx context.Context, ev Event) {
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ip := ev.SourceIP
	resp, err := w.Client.AccessRequest(cctx, client.AccessRequest{
		Context:  "ssh",
		Mode:     config.ModeNotify,
		Username: ev.Username,
		SourceIP: &ip,
		Hostname: w.Hostname,
		TTY:      "ssh",
	})

	line := auditlog.Line{Decision: auditlog.Allow, Context: "ssh", User: ev.Username, RHost: ev.SourceIP, Host: w.Hostname}
	switch {
	case err == nil:
		line.Reason, line.Request = "notify", resp.RequestID
	case client.IsRejected(err):
		line.Reason = "rejected"
	default:
		line.Reason = "unreachable"
	}
	w.Log.Log(line)
}
