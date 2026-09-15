package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/simootaz/ssh-sentinel/web"
)

// The dashboard (docs/architecture.md, section 3.6) is served from the
// files embedded in the web module, at GET /dashboard/ and
// GET /dashboard/{file}. No auth on the files: they hold nothing secret, and
// every call they make carries the admin token the user typed.
//
// The whole folder is read once at start into memory (a few tens of KB),
// with one ETag per file, so serving it costs a map lookup.

// dashboardCSP allows the dashboard's own origin and nothing else: no inline
// script or style, no other host, never framed. The dashboard is written to
// this policy (no inline handlers, no style attributes).
const dashboardCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// staticFile is one embedded file, ready to serve.
type staticFile struct {
	body        []byte
	contentType string
	etag        string
}

// contentTypes by extension. Everything the dashboard ships is here; an
// unknown extension is served as bytes, never sniffed (nosniff below).
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".txt":   "text/plain; charset=utf-8",
	".woff2": "font/woff2",
	".map":   "application/json",
}

// loadDashboard reads every file of fsys into memory. A nil fsys means the
// embedded dashboard; tests pass their own.
func loadDashboard(fsys fs.FS) (map[string]staticFile, error) {
	if fsys == nil {
		fsys = web.FS
	}
	files := map[string]staticFile{}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		ct, ok := contentTypes[strings.ToLower(path.Ext(name))]
		if !ok {
			ct = "application/octet-stream"
		}
		files[name] = staticFile{body: body, contentType: ct, etag: `"` + hex.EncodeToString(sum[:8]) + `"`}
		return nil
	})
	return files, err
}

// registerDashboard adds the three routes to mux.
func (h *Handler) registerDashboard(mux *http.ServeMux) {
	// /dashboard without the slash: redirect so that the relative links of
	// index.html (app.css, js/app.js) resolve under /dashboard/.
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		setDashboardHeaders(w)
		http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
	})
	mux.HandleFunc("GET /dashboard/{$}", func(w http.ResponseWriter, r *http.Request) {
		h.serveDashboardFile(w, r, "index.html")
	})
	mux.HandleFunc("GET /dashboard/{file...}", func(w http.ResponseWriter, r *http.Request) {
		h.serveDashboardFile(w, r, r.PathValue("file"))
	})
}

// serveDashboardFile answers with one embedded file, or the contract's JSON
// 404 for anything that is not in the embedded set. The mux has already
// cleaned the path (no .. segments reach here), and the map lookup does the
// rest: only what is embedded can be served, never a Go file of the module.
func (h *Handler) serveDashboardFile(w http.ResponseWriter, r *http.Request, name string) {
	setDashboardHeaders(w)
	f, ok := h.dashboard[name]
	if !ok || !fs.ValidPath(name) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", f.contentType)
	w.Header().Set("ETag", f.etag)
	// Always revalidate: a redeploy must show up on the next reload, and the
	// ETag makes that revalidation a 304 with no body.
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(f.body)
	}
}

// etagMatches reports whether an If-None-Match header names etag. Weak
// validators (W/"...") and lists are accepted, as the header allows.
func etagMatches(header, etag string) bool {
	if header == "*" {
		return true
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "W/"))
		if part == etag {
			return true
		}
	}
	return false
}

// setDashboardHeaders adds the security headers of every dashboard answer,
// files, redirect and 404 alike.
func setDashboardHeaders(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Content-Security-Policy", dashboardCSP)
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("X-Frame-Options", "DENY")
	hdr.Set("Referrer-Policy", "no-referrer")
}
