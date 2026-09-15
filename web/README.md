# web

The dashboard: a single-page app served by the backend at `/dashboard`, on every deployment, with nothing to host (`docs/architecture.md`, section 3.6 and decisions 30 and 31). Plain HTML, one stylesheet, JavaScript modules. No framework, no build step, no dependency.

## What it does

Log in with an admin token (per-admin token from `POST /admins`, or the bootstrap `ADMIN_TOKEN`). The token stays in the tab's session storage and goes out as the bearer of every call; closing the tab forgets it. No cookie, so no CSRF surface: the API is what it always was.

| Screen | Route(s) used | What |
|---|---|---|
| Pending | `GET /history?status=pending` every 2 s, `POST /verdict` | live requests with a countdown from `expires_at`, Deny / Approve / Always allow with a TTL picker (1 h, 24 h, custom, permanent). First verdict wins: a `409` shows who decided |
| History | `GET /history` | filters (server, user, context, status, with a recording), paging with `next_before`, who decided (`decided_by_admin` and the phone label when present), link to the recording |
| Whitelist | `GET/POST/DELETE /whitelist` | list, add with context, server and TTL, remove |
| Blocked IPs | `GET/DELETE /blocked-ips` | list, unblock |
| Geo rules | `GET/POST/DELETE /geo-rules` | list, add a country, remove |
| Admins | `GET/POST /admins`, `POST /admins/{id}/rotate`, `DELETE /admins/{id}` | list, add (token shown once), rotate (token shown once, re-enables a disabled admin), disable |
| Servers | `GET /servers`, `POST /servers/{id}/rotate`, `DELETE /servers/{id}` | list, rotate (token shown once), revoke |
| Recording | `GET /recordings/{request_id}` | the typescript as plain text (escape sequences stripped, `\r` and backspaces applied), a raw view, download |

The dashboard is a client of the contract like the phone app: everything it calls is a route of section 5, never a private endpoint. A backend that does not serve a route yet (the v1.1 routes on a 1.0.0 backend) answers `404`, and the screen shows that line instead of a list; nothing else breaks. A `501` on a recording means the deployment has no recording storage (Scaleway today).

Polling: one `GET /history?status=pending` every 2 s while logged in, paused while the tab is hidden, so an open dashboard costs half a request per second per tab.

## Files

| Path | Content |
|---|---|
| `index.html` | the page: top bar, navigation, one `<main>` the views render into |
| `app.css` | the stylesheet, light and dark from the system |
| `icon.svg` | favicon |
| `js/app.js` | entry point: login gate, hash router (`#pending`, `#history`, `#recording/<id>`, ...), the 2 s poller |
| `js/api.js` | base URL, token storage, `request()`, one function per route, `ApiError` |
| `js/dom.js` | DOM helpers: elements are built with `createElement` and `textContent`, never from HTML strings; tables, badges, the TTL picker, the token panel |
| `js/format.js` | dates, durations, sizes, TTL to seconds, who decided |
| `js/ansi.js` | typescript to plain text |
| `js/views/*.js` | one module per screen, `render(container, ctx)` returning an optional cleanup function |
| `test/*.test.js` | unit tests of the pure modules, `node --test` |
| `embed.go`, `go.mod` | the Go side: this folder is a module the backend embeds |

## How it is served

`go:embed` cannot reach outside the Go module that uses it, and the backend module lives in `backend/`. So this folder is a Go module of its own (`github.com/simootaz/ssh-sentinel/web`) whose only Go file embeds `index.html`, `app.css`, `icon.svg` and `js/`. `backend/go.mod` requires it with `replace github.com/simootaz/ssh-sentinel/web => ../web`, and `backend/internal/handler/dashboard.go` serves the embedded tree:

- `GET /dashboard` redirects to `/dashboard/`, `GET /dashboard/` is `index.html`, `GET /dashboard/{file}` the rest. Anything not embedded is the contract's JSON `404`, so no Go file, test or doc of this folder is ever served.
- No auth on the files. Every API call the page makes carries the token the user typed.
- Headers on every answer: `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`. Inline scripts, inline styles and any other origin are refused by the browser; write the dashboard accordingly (no `onclick=""`, no `style=""`, no CDN).
- `Cache-Control: no-cache` with an ETag per file: a reload after a redeploy gets the new files, an unchanged file is a `304`.

Consequences for the builds: the Docker build context is the repository root (`backend/Dockerfile` copies `backend/` and `web/`), and the Scaleway zip needs `go mod vendor` in `backend\` first so that this module travels inside `vendor/` (`backend/README.md`, Build).

## Run it

Start the backend from source or with compose (`backend/TESTING.md`, section 3, or `infra/docker-compose/`) and open `http://localhost:8080/dashboard/`. Log in with the `ADMIN_TOKEN` of that backend. HTTPS in production: the dashboard is served by the same origin as the API, whatever terminates TLS in front of it.

Editing: change a file here, rebuild the backend (`go build ./...` in `backend\`, or `docker compose build`), reload. There is nothing to bundle.

## Test

Unit tests of the pure modules (escape stripping, formatting, base URL and query building), Node 20 or newer, no package to install:

```powershell
cd web
node --test test/*.test.js
```

The Go tests of the serving side are in `backend/internal/handler/dashboard_test.go` (`go test ./internal/handler -run Dashboard` from `backend\`): redirect, no auth on the files, security headers, every embedded file served with the right content type and a working ETag, JSON `404` for anything else, path traversal, a consistency check that every asset referenced by `index.html` and every relative `import` of the modules exists in the embedded tree (and that no module uses `innerHTML`).

Everything that needs a browser is a manual test: `TESTING.md` in this folder walks every screen against a local backend.
