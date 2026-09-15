package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/simootaz/ssh-sentinel/agent/internal/auditlog"
	"github.com/simootaz/ssh-sentinel/agent/internal/check"
	"github.com/simootaz/ssh-sentinel/agent/internal/client"
	"github.com/simootaz/ssh-sentinel/agent/internal/config"
)

// runCheck is the PAM hook. It reads what pam_exec puts in the environment, loads the config
// and hands everything to check.Run. A missing or broken config is not fatal: the break-glass
// file and the cache keep working and the backend counts as unreachable.
func runCheck(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	context := fs.String("context", "ssh", "ssh or sudo")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "usage: ssh-sentinel check [--context=ssh|sudo]")
		return 2
	}
	if *context != "ssh" && *context != "sudo" {
		fmt.Fprintf(stderr, "check: --context must be ssh or sudo, got %q\n", *context)
		return 2
	}

	paths := config.DefaultPaths()
	deps := check.Deps{
		Context:        *context,
		Mode:           config.ModeEnforce,
		User:           os.Getenv("PAM_USER"),
		RHost:          os.Getenv("PAM_RHOST"),
		TTY:            os.Getenv("PAM_TTY"),
		BreakGlassPath: paths.BreakGlass,
		CachePath:      paths.Cache,
		Timeout:        30 * time.Second,
		Now:            time.Now,
		Log:            auditlog.NewSystem(),
	}

	cfg, err := config.Load(paths.Config)
	if err != nil {
		fmt.Fprintf(stderr, "check: %v (continuing with the break-glass file and the cache only)\n", err)
	} else {
		deps.Client = client.New(cfg.BackendURL, cfg.ServerToken)
		deps.Mode = cfg.Mode
		deps.Timeout = cfg.Timeout()
		deps.Hostname = cfg.Hostname
	}
	if deps.Hostname == "" {
		deps.Hostname, _ = os.Hostname()
	}
	if *context == "sudo" {
		deps.Command = check.SudoCommand()
	}
	return check.Run(deps)
}
