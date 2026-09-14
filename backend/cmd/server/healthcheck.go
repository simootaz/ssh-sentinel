package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/app"
)

// healthcheckTimeout is the whole budget of one probe; the Dockerfile gives
// the probe 5 s, so the binary answers before Docker gives up on it.
const healthcheckTimeout = 3 * time.Second

// runHealthcheck is the container HEALTHCHECK: the runtime image has no
// shell and no curl, so the binary probes itself. Exit 0 when GET /healthz
// answers 200, 1 otherwise. Reads LISTEN_ADDR only, to find the port.
func runHealthcheck(getenv func(string) string, stderr io.Writer) int {
	url, err := healthURL(getenv("LISTEN_ADDR"))
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}

	client := &http.Client{Timeout: healthcheckTimeout}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "healthcheck: %s answered %d\n", url, resp.StatusCode)
		return 1
	}
	return 0
}

// healthURL builds the probe URL from LISTEN_ADDR. Only the port is kept:
// the server listens on every interface in the container, and 127.0.0.1 is
// the one address that always reaches it from inside.
func healthURL(listenAddr string) (string, error) {
	if listenAddr == "" {
		listenAddr = app.DefaultListenAddr
	}
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("LISTEN_ADDR %q: %w", listenAddr, err)
	}
	return "http://127.0.0.1:" + port + "/healthz", nil
}
