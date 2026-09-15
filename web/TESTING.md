# Testing the dashboard

Three layers: the Go tests of the serving side, the node tests of the pure JavaScript modules, and a manual walk through every screen in a browser against a local backend. Run the first two after any change; run the walk before a release and after a change to a view.

## 1. Automated

From `backend\`, the serving side (redirect, no auth on the files, security headers, every embedded file with its content type and ETag, JSON 404s, traversal, consistency of the references between `index.html` and the modules):

```powershell
go test ./internal/handler -run Dashboard -count=1
```

From `web\`, the pure modules (escape stripping and terminal rendering, formatting, TTL mapping, base URL and query building), Node 20 or newer, nothing to install:

```powershell
node --test test/*.test.js
```

## 2. Local stack

The backend from source with PostgreSQL in Docker, as in `backend/TESTING.md` sections 2 to 4, push disabled: curl plays the agent, the dashboard plays the phone. In short, PowerShell from `backend\`:

```powershell
docker run --rm -d --name ssh-sentinel-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:16
$env:DATABASE_URL = "postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
$env:ADMIN_TOKEN = "dev-admin-token"
go run .\cmd\server serve
```

In a second terminal (Git Bash), enrol a server and define the `agent` helper of `backend/TESTING.md` section 4:

```bash
cd backend
export DATABASE_URL="postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
SERVER_TOKEN=$(go run ./cmd/server enroll --name web-01 --os linux)
BASE=http://localhost:8080
agent() { curl -s -w 'HTTP %{http_code}\n' -X POST "$BASE/access-request" -H "Authorization: Bearer $SERVER_TOKEN" -H 'Content-Type: application/json' -d "$1"; }
```

Open `http://localhost:8080/dashboard` in a browser with the developer tools console open: every step below must leave the console without errors (the browser's own line for a 401 or 404 answer is expected where the step says so).

## 3. Walk

### 3.1 Login

- `/dashboard` redirects to `/dashboard/`. The login form is alone on the page: no navigation, no Log out button.
- A wrong token: `The token was rejected (HTTP 401). Log in again.` above the field, the field keeps focus.
- `dev-admin-token`: the navigation appears, the Pending screen says `Nothing pending`. The tab title becomes `Pending · ssh-sentinel`.
- Reload the tab: still logged in (session storage). Open the same URL in a new tab: login again (session storage is per tab). Close and reopen the tab: login again.
- Log out: back to the form, `#` cleared from the URL.

### 3.2 Pending, approve

```bash
agent '{"context": "sudo", "mode": "enforce", "username": "deploy", "source_ip": null, "hostname": "web-01", "tty": "pts/0", "command": "sudo systemctl restart nginx"}'
```

Within 2 s a card appears with the `sudo` badge, `deploy on web-01`, `Source: local`, `Host: web-01 (pts/0)`, the command, and a countdown from about 28 s that turns red under 10 s. The Pending link in the navigation shows `1`.

Click Approve: the buttons grey out, then `approved by you`. The curl terminal shows `"verdict":"approve","reason":"admin"`, `HTTP 200`. The card goes after 8 s; the counter goes back to nothing.

### 3.3 Pending, deny and always allow

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}'
```

`Source: 203.0.113.42`, no command. Deny: `denied by you`, curl gets `"verdict":"deny"`.

Send it again. Always allow…: a line opens with the TTL choice (permanent, 1 hour, 24 hours, custom with a number and a unit). Pick 1 hour, Confirm: `approved by you` then `Whitelisted deploy for ssh on web-01, expires: <time> (in 1 h)`. Custom with `0` or a blank number shows `duration must be a positive number` and sends nothing.

Send it a third time: curl answers at once with `"reason":"whitelist"` and no card appears (the backend approved it without a push).

### 3.4 Pending, first verdict wins

Two tabs of the dashboard, or one tab plus curl as a phone. Send a request for another user:

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "alice", "source_ip": "198.51.100.7", "hostname": "web-01", "tty": "ssh", "command": null}'
```

Decide it from the other tab (or with `phone POST /verdict` of `backend/TESTING.md`), then click Approve in the first tab within the same 2 s poll window: `Already decided: <status> by <admin or device>` in the card, buttons disabled, card gone after 8 s. If the poll ran first, the card simply disappears: that is the same information, one round later.

Let a request run out: the countdown reaches `expired`, the buttons grey out, the card goes at the next poll once the backend marks it `timeout`.

### 3.5 History

Open History. The requests above are listed newest first with time, server, context badge, user, source and geo when known, command or tty, status badge, who decided (`admin` on a 1.0.0 backend, the admin name and phone label on a v1.1 one), decided at, recording (`—`).

Filters: user `deploy` + Search keeps only deploy; status `approved`; context `sudo`; server `nope` gives `No request matches.`; `with a recording` gives an empty list on a backend without recordings. Load more appears only when a page is full (50 rows; send more requests or lower `PAGE` in `history.js` to see it) and appends the next page.

### 3.6 Whitelist

The entry of 3.3 is listed with its expiry and `From request`. Add `ansible`, ssh, server blank, permanent: the row shows `every server` and `permanent`. Add the same again: the form reports the refreshed entry, no duplicate row. Server name `nope`: `HTTP 404: ...` under the form. Remove an entry: a confirmation, then the row is gone.

### 3.7 Blocked IPs

Deny three requests from the same IP (three times `agent` with `198.51.100.7` and Deny in the dashboard; the third answer shows `IP 198.51.100.7 is now blocked (3 denials)` in the card). Open Blocked IPs: the row with denials, hits, `until unblocked`. Send the request again: curl gets `"reason":"blocked_ip"` at once and `Hits since block` becomes 1 after a refresh. Unblock: confirmation, then the list is empty.

### 3.8 Geo rules

Add `kp` with a note: the row shows `KP`. Add `kp` again: `HTTP 409: ...` under the form. Add `xx`: `HTTP 400: ...`. Remove: confirmation, list empty.

### 3.9 Admins and Servers (v1.1 routes)

On a backend without these routes (a 1.0.0 backend, or this branch alone): both screens show `HTTP 404: not found` where the list would be, the forms stay usable and Add answers with the same line. Nothing else breaks and the other screens keep working.

On a backend with them: Add `alice`: a panel shows the token once with Copy and Dismiss, the list has `alice` active with `never` as last seen. Use that token in a second tab: it logs in, `last seen` updates. Rotate: confirmation, a new panel, the second tab gets `The token was rejected (HTTP 401)` on its next poll and shows the login form. Disable: confirmation, the row shows `disabled <time> ago`, Rotate and re-enable is the only action left. Servers: `web-01` enrolled with last seen; Rotate gives a token panel and the next `agent` call gets `401` until the new token is used; Revoke asks for confirmation and marks the row `revoked` with no actions.

### 3.10 Recording (v1.1 route)

Without recording storage or on a backend without the route: `#recording/<id>` shows `This request has no recording...` (404) or `Recordings are not supported on this deployment` (501); the back link returns to History.

With a recording (an agent v1.1 upload, or `curl --data-binary @typescript.gz -H 'Content-Encoding: gzip' ... POST /recordings/<id>?started_at=...&ended_at=...`): History shows `view (<size>)` in the Recording column; the viewer shows the session as plain text with colours and cursor moves removed and progress lines collapsed; Show raw shows the control characters as `␛[0m`, `␍`; Download saves `<request id>.log`. A truncated upload shows the `truncated by the agent` badge.

### 3.11 Backend down

Stop the backend (Ctrl+C). The top bar shows `poll: backend unreachable: ...` in red within 2 s, the Pending screen says `Poll failed`. Start it again: the message clears at the next poll, nothing to reload. Restart with another `ADMIN_TOKEN`: the next poll gets 401 and the login form comes back with the message.

### 3.12 Hidden tab

Switch to another tab for a minute: the backend log shows no `GET /history` from the dashboard while it is hidden, and one right away when the tab comes back.

## 4. Cleanup

Ctrl+C the backend, `docker stop ssh-sentinel-pg`.
