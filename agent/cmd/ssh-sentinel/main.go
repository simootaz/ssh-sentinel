// Command ssh-sentinel is the server-side agent: the PAM hook (check), the whitelist cache
// refresh (sync) and the Windows event-log watcher (watch).
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

const usage = `usage: ssh-sentinel <command> [flags]

commands:
  check [--context=ssh|sudo]   PAM hook run by pam_exec: exit 0 = allow, 1 = deny
  sync                         refresh the local whitelist cache from the backend
  watch [--once]               Windows: report SSH logins from the event log, notify-only
  version                      print the version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches to the subcommand and returns the process exit code. Usage errors exit 2,
// which pam_exec treats like any other non-zero code: the login is denied.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stderr)
	case "sync":
		return runSync(args[1:], stdout, stderr)
	case "watch":
		return runWatch(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "ssh-sentinel "+version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return 2
	}
}
