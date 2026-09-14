// Command server is the backend binary of the container: the API itself
// ("serve", the default), the migrations ("migrate"), server enrollment
// ("enroll"), the container health probe ("healthcheck") and "version".
// Configuration comes from the environment only (README.md, Configuration).
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.0".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run dispatches on the subcommand and returns the exit code. Everything
// main needs (arguments, environment, output streams) is passed in so that
// the tests can call it without a real process.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "serve":
		return runServe(getenv, stdout)
	case "migrate":
		return runMigrate(getenv, stdout, stderr)
	case "enroll":
		return runEnroll(args, getenv, stdout, stderr)
	case "healthcheck":
		return runHealthcheck(getenv, stderr)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: server [command]

commands:
  serve        run the API (default). Applies the pending migrations first.
  migrate      apply the pending migrations and exit
  enroll       register a server and print its token once on stdout:
                 enroll --name <server name> [--os linux|windows|darwin]
  healthcheck  GET /healthz on the local listener; exit 0 when it answers 200
  version      print the version

environment:
  serve reads every variable of README.md (DATABASE_URL and ADMIN_TOKEN are
  required). migrate and enroll need DATABASE_URL only. healthcheck reads
  LISTEN_ADDR to find the port.
`)
}
