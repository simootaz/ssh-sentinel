package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// noEnv is an empty environment.
func noEnv(string) string { return "" }

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// call runs the binary's dispatcher and returns the exit code and both streams.
func call(args []string, getenv func(string) string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, getenv, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := call([]string{"version"}, noEnv)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if strings.TrimSpace(out) != version {
		t.Errorf("stdout %q, want %q", out, version)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, out, errOut := call([]string{"bogus"}, noEnv)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout should be empty, got %q", out)
	}
	if !strings.Contains(errOut, "usage:") || !strings.Contains(errOut, "bogus") {
		t.Errorf("stderr should show the usage and the bad command, got %q", errOut)
	}
}

func TestHelp(t *testing.T) {
	code, out, _ := call([]string{"--help"}, noEnv)
	if code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("exit %d, stdout %q", code, out)
	}
}

func TestServeIsTheDefaultAndNeedsConfig(t *testing.T) {
	// Without DATABASE_URL and ADMIN_TOKEN serve stops at the configuration
	// step, which is as far as a test without a database can go.
	for _, args := range [][]string{nil, {"serve"}} {
		code, out, _ := call(args, noEnv)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
		if !strings.Contains(out, "DATABASE_URL") || !strings.Contains(out, "ADMIN_TOKEN") {
			t.Errorf("%v: the JSON log should name the missing variables, got %q", args, out)
		}
	}
}

func TestMigrateNeedsDatabaseURL(t *testing.T) {
	code, _, errOut := call([]string{"migrate"}, noEnv)
	if code != 1 || !strings.Contains(errOut, "DATABASE_URL") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestEnrollArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // expected on stderr
	}{
		{"missing name", []string{"enroll"}, "--name is required"},
		{"empty name", []string{"enroll", "--name", "  "}, "--name is required"},
		{"bad os", []string{"enroll", "--name", "web-01", "--os", "freebsd"}, "--os must be"},
		{"unknown flag", []string{"enroll", "--name", "web-01", "--colour", "red"}, "flag provided but not defined"},
	}
	for _, c := range cases {
		code, out, errOut := call(c.args, noEnv)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2", c.name, code)
		}
		if out != "" {
			t.Errorf("%s: stdout must stay empty (only the token goes there), got %q", c.name, out)
		}
		if !strings.Contains(errOut, c.want) {
			t.Errorf("%s: stderr %q does not contain %q", c.name, errOut, c.want)
		}
	}
}

func TestEnrollNeedsDatabaseURL(t *testing.T) {
	code, out, errOut := call([]string{"enroll", "--name", "web-01", "--os", "windows"}, noEnv)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout must stay empty, got %q", out)
	}
	if !strings.Contains(errOut, "DATABASE_URL") {
		t.Errorf("stderr %q should mention DATABASE_URL", errOut)
	}
}

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func TestNewToken(t *testing.T) {
	a, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	// 32 bytes in base64url without padding is 43 characters, none of
	// which needs quoting in a config file or a header.
	if !tokenRe.MatchString(a) {
		t.Errorf("token %q is not 43 base64url characters", a)
	}
	if a == b {
		t.Error("two tokens are identical")
	}
}

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		"":               "http://127.0.0.1:8080/healthz",
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"localhost:9001": "http://127.0.0.1:9001/healthz",
	}
	for addr, want := range cases {
		got, err := healthURL(addr)
		if err != nil {
			t.Errorf("%q: unexpected error %v", addr, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", addr, got, want)
		}
	}
	if _, err := healthURL("no-port"); err == nil {
		t.Error("an address without a port should be an error")
	}
}

// listenAddrOf returns ":port" of a test server, as LISTEN_ADDR would be set.
func listenAddrOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return ":" + port
}

func TestHealthcheck(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","db":"ok"}`))
	}))
	defer healthy.Close()

	degraded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"degraded","db":"error"}`))
	}))
	defer degraded.Close()

	// A closed listener: nothing answers on that port any more.
	gone := httptest.NewServer(http.NotFoundHandler())
	goneAddr := listenAddrOf(t, gone)
	gone.Close()

	cases := []struct {
		name string
		addr string
		want int
	}{
		{"healthy", listenAddrOf(t, healthy), 0},
		{"degraded", listenAddrOf(t, degraded), 1},
		{"nothing listening", goneAddr, 1},
		{"bad LISTEN_ADDR", "nonsense", 1},
	}
	for _, c := range cases {
		code, _, errOut := call([]string{"healthcheck"}, envOf(map[string]string{"LISTEN_ADDR": c.addr}))
		if code != c.want {
			t.Errorf("%s: exit %d, want %d (stderr %q)", c.name, code, c.want, errOut)
		}
	}
}
