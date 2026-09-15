package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/auth"
	"github.com/simootaz/ssh-sentinel/backend/internal/db"
	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

// tokenBytes is the size of a server token before encoding: 32 random
// bytes, base64url without padding, as docs/architecture.md section 6 says.
const tokenBytes = 32

const enrollTimeout = 30 * time.Second

// runEnroll is the server enrollment of section 6: generate the token,
// store its hash and the server name, print the plain token once. There is
// no HTTP endpoint for this on purpose. Only the token goes to stdout, so a
// script can capture it: everything else is on stderr. Needs DATABASE_URL.
//
// Exit codes: 0 enrolled, 1 database or duplicate name, 2 bad arguments.
func runEnroll(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "server name, shown in the app (required)")
	osName := fs.String("os", model.OSLinux, "operating system: linux, windows or darwin")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	*name = strings.TrimSpace(*name)
	if *name == "" {
		fmt.Fprintln(stderr, "enroll: --name is required")
		fs.Usage()
		return 2
	}
	if !model.ValidOS(*osName) {
		fmt.Fprintf(stderr, "enroll: --os must be linux, windows or darwin, got %q\n", *osName)
		return 2
	}

	url := getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(stderr, "enroll: DATABASE_URL is not set")
		return 1
	}

	token, err := newToken()
	if err != nil {
		fmt.Fprintln(stderr, "enroll: generate token:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), enrollTimeout)
	defer cancel()

	pg, err := db.Open(ctx, url)
	if err != nil {
		fmt.Fprintln(stderr, "enroll: open database:", err)
		return 1
	}
	defer pg.Close()

	srv, err := pg.CreateServer(ctx, *name, auth.HashToken(token), *osName, time.Now().UTC())
	if errors.Is(err, db.ErrConflict) {
		fmt.Fprintf(stderr, "enroll: a server named %q is already enrolled; delete its row from the servers table to enroll it again\n", *name)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "enroll: create server:", err)
		return 1
	}

	fmt.Fprintln(stdout, token)
	fmt.Fprintf(stderr, "server %q enrolled (id %s). The token above is shown once; put it in the agent's config file.\n", srv.Name, srv.ID)
	return 0
}

// newToken returns a fresh server token: 32 bytes from the OS random
// source, base64url without padding (43 characters, safe in a config file
// and in an Authorization header).
func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
