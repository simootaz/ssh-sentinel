package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
	"github.com/simootaz/ssh-sentinel/agent/internal/watch"
)

// onceLookback is how far back "watch --once" looks, so a login made just before running it
// shows up in the report.
const onceLookback = 5 * time.Minute

// runWatch is the Windows event-log watcher, notify-only. The mode sent to the backend is
// always notify, whatever the config says.
func runWatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	once := fs.Bool("once", false, "report the logins of the last five minutes and exit")
	channel := fs.String("channel", "OpenSSH/Operational", "event log channel to follow")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: ssh-sentinel watch [--once] [--channel NAME]")
		return 2
	}

	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.Config)
	if err != nil {
		fmt.Fprintf(stderr, "watch: %v\n", err)
		return 1
	}

	var lookback time.Duration
	if *once {
		lookback = onceLookback
	}
	src := watch.NewSystemSource(*channel, lookback)
	if src == nil {
		fmt.Fprintln(stderr, "watch is only supported on Windows")
		return 2
	}

	host := cfg.Hostname
	if host == "" {
		host, _ = os.Hostname()
	}
	w := &watch.Watcher{
		Source:   src,
		Client:   client.New(cfg.BackendURL, cfg.ServerToken),
		Hostname: host,
		Log:      auditlog.NewSystem(),
		Poll:     cfg.WatchPoll(),
		Timeout:  10 * time.Second,
		Errors:   stderr,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *once {
		n, err := w.RunOnce(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "watch: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "reported %d logins\n", n)
		return 0
	}
	fmt.Fprintf(stdout, "watching %s every %s, reporting to %s\n", *channel, cfg.WatchPoll(), cfg.BackendURL)
	_ = w.Run(ctx)
	return 0
}
