package handler

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
	"github.com/simootaz/ssh-sentinel/web"
)

// get sends a GET without following redirects and returns the response.
func (e *env) get(p string, hdr map[string]string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+p, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("GET %s: %v", p, err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestDashboardRedirectsToSlash(t *testing.T) {
	e := newEnv(t)
	resp := e.get("/dashboard", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/dashboard/" {
		t.Fatalf("Location %q, want /dashboard/", loc)
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("redirect without CSP")
	}
}

func TestDashboardIndexNeedsNoAuth(t *testing.T) {
	e := newEnv(t)
	for _, hdr := range []map[string]string{nil, {"Authorization": "Bearer wrong"}} {
		resp := e.get("/dashboard/", hdr)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Fatalf("content type %q", ct)
		}
		body := readAll(t, resp)
		if !strings.Contains(body, "<title>ssh-sentinel</title>") || !strings.Contains(body, `src="js/app.js"`) {
			t.Fatalf("index.html not served: %.200s", body)
		}
	}
}

func TestDashboardSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	resp := e.get("/dashboard/", nil)
	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP %q allows inline or eval", csp)
	}
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-cache",
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// Every embedded file is served with the right content type and an ETag
// that turns the next load into a 304.
func TestDashboardServesEveryEmbeddedFile(t *testing.T) {
	e := newEnv(t)
	n := 0
	err := fs.WalkDir(web.FS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		resp := e.get("/dashboard/"+name, nil)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", name, resp.StatusCode)
			return nil
		}
		want, ok := contentTypes[strings.ToLower(path.Ext(name))]
		if !ok {
			t.Errorf("%s: extension without a content type", name)
		}
		if got := resp.Header.Get("Content-Type"); got != want {
			t.Errorf("%s: content type %q, want %q", name, got, want)
		}
		etag := resp.Header.Get("ETag")
		if etag == "" {
			t.Errorf("%s: no ETag", name)
		}
		body := readAll(t, resp)
		raw, _ := fs.ReadFile(web.FS, name)
		if body != string(raw) {
			t.Errorf("%s: body differs from the embedded file", name)
		}
		again := e.get("/dashboard/"+name, map[string]string{"If-None-Match": etag})
		if again.StatusCode != http.StatusNotModified {
			t.Errorf("%s: with If-None-Match status %d, want 304", name, again.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 5 {
		t.Fatalf("only %d embedded files, the dashboard looks incomplete", n)
	}
}

func TestDashboardUnknownFileIsJSON404(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{
		"/dashboard/nope.js",
		"/dashboard/js/",
		"/dashboard/js",
		"/dashboard/embed.go", // the Go files of the web module are not embedded
		"/dashboard/go.mod",
		"/dashboard/README.md",
		"/dashboard/test/ansi.test.js",
	} {
		resp := e.get(p, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content type %q", p, ct)
		}
		if resp.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: 404 without CSP", p)
		}
	}
}

// Paths that try to climb out of the dashboard never serve anything from
// the module: the mux cleans them (301 to the clean path) or they miss.
func TestDashboardTraversal(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{
		"/dashboard/../go.mod",
		"/dashboard/js/../../go.mod",
		"/dashboard/%2e%2e/go.mod",
		"/dashboard/js/%2e%2e/%2e%2e/embed.go",
	} {
		resp := e.get(p, nil)
		if resp.StatusCode == http.StatusOK {
			t.Errorf("%s: served with 200: %.80s", p, readAll(t, resp))
		}
	}
}

func TestDashboardWrongMethod(t *testing.T) {
	e := newEnv(t)
	st, body := e.do(http.MethodPost, "/dashboard/", "", nil)
	mustStatus(t, st, http.StatusMethodNotAllowed, body)
	if decode[model.ErrorResponse](t, body).Error == "" {
		t.Fatalf("body %s", body)
	}
}

// A handler built on a tree without index.html still starts and answers 404.
func TestDashboardMissingFilesAnswer404(t *testing.T) {
	e := newEnvWith(t, envOptions{Dashboard: fstest.MapFS{"only.txt": {Data: []byte("x")}}})
	resp := e.get("/dashboard/", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("index: status %d, want 404", resp.StatusCode)
	}
	resp = e.get("/dashboard/only.txt", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("only.txt: status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

// The dashboard is a client of the API with the admin token: the files
// need no token, the API still does.
func TestDashboardAPIAuthUnchanged(t *testing.T) {
	e := newEnv(t)
	st, body := e.do(http.MethodGet, "/history?status=pending", "", nil)
	mustStatus(t, st, http.StatusUnauthorized, body)
	st, body = e.admin(http.MethodGet, "/history?status=pending", nil)
	mustStatus(t, st, http.StatusOK, body)
}

func TestEtagMatches(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{`"abc"`, true},
		{`W/"abc"`, true},
		{`"x", "abc"`, true},
		{`*`, true},
		{`"abd"`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := etagMatches(c.header, `"abc"`); got != c.want {
			t.Errorf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

// Consistency of the embedded tree: every asset index.html references and
// every relative import of the JavaScript modules resolves to an embedded
// file. A typo in a path would otherwise only show in a browser.
func TestDashboardReferencesResolve(t *testing.T) {
	index, err := fs.ReadFile(web.FS, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	refs := regexp.MustCompile(`(?:src|href)="([^"#:]+)"`)
	for _, m := range refs.FindAllStringSubmatch(string(index), -1) {
		if _, err := fs.Stat(web.FS, m[1]); err != nil {
			t.Errorf("index.html references %q: %v", m[1], err)
		}
	}
	imports := regexp.MustCompile(`from\s+'(\.[^']+)'`)
	err = fs.WalkDir(web.FS, "js", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := fs.ReadFile(web.FS, name)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "innerHTML") {
			t.Errorf("%s uses innerHTML; build the DOM with the dom.js helpers instead", name)
		}
		for _, m := range imports.FindAllStringSubmatch(string(src), -1) {
			target := path.Join(path.Dir(name), m[1])
			if _, err := fs.Stat(web.FS, target); err != nil {
				t.Errorf("%s imports %q (%s): %v", name, m[1], target, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
