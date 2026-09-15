// Package web is the dashboard: index.html, one stylesheet and plain
// JavaScript modules, no framework and no build step (docs/architecture.md,
// section 3.6). The backend embeds this folder and serves it at /dashboard,
// so the dashboard is part of every deployment with nothing to host.
//
// It is a Go module of its own because go:embed cannot reach outside the
// module that uses it, and the backend module lives in backend/. The backend
// requires this module with a replace directive pointing at ../web.
package web

import "embed"

// FS holds the files served under /dashboard: what the browser loads and
// nothing else. The Go files, the tests and the docs of this folder stay out.
//
//go:embed index.html app.css icon.svg js
var FS embed.FS
