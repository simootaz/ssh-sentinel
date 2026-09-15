# Testing the backend

How to run the unit tests, and a curl walkthrough of every route of CONTRACT v1 (`docs/architecture.md`, section 5) against a local backend, with no server and no phone: curl plays the agent (server token) and the phone (admin token). Run the walkthrough after a change to the backend, and after a deployment with `BASE` pointed at it.

## 1. What it covers, prerequisites

Covered:

- the unit tests, with and without a database
- a local stack with nothing from `infra/`: PostgreSQL in Docker, the backend from source
- every route: health, phones, an SSH login approved, denied, always-allowed and timed out, sudo, two phones answering, the whitelist, auto-block, geo rules, notify mode, history paging and filters, auth errors

Prerequisites:

- Go 1.22 or newer
- Docker, for PostgreSQL only
- `curl` (Git for Windows ships one)
- `jq`, optional: the one-liners that capture ids use it (`winget install jqlang.jq` on Windows); without it, copy the ids from the answers by hand
- a shell: the curl part is bash. On Windows run it from Git Bash; on Linux any shell. The backend is started from PowerShell on Windows or bash on Linux, both are shown.

Conventions: `$BASE` is the backend URL, `$ADMIN_TOKEN` what the phones send, `$SERVER_TOKEN` what the agent sends. Ids and timestamps in the expected answers are examples; check the other fields and the status code.

## 2. Unit tests

From `backend\`:

```powershell
go test ./...
```

Handler and rules tests use an in-memory store, a fake FCM client and a fake clock: no network, no database. Database tests need a PostgreSQL and skip themselves while `TEST_DATABASE_URL` is unset. Start one and set the variable, PowerShell:

```powershell
docker run --rm -d --name ssh-sentinel-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:16
$env:TEST_DATABASE_URL = "postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
go test ./... -count=1
```

bash:

```bash
docker run --rm -d --name ssh-sentinel-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:16
export TEST_DATABASE_URL="postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
go test ./... -count=1
```

Keep the container running: section 3 uses it. Before a release, run the same against `postgres:14`, the floor the contract promises.

## 3. Local stack

### 3.1 PostgreSQL

If it is not running from section 2:

```
docker run --rm -d --name ssh-sentinel-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:16
```

### 3.2 The backend, from source

From `backend\`, PowerShell:

```powershell
$env:DATABASE_URL = "postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
$env:ADMIN_TOKEN = "dev-admin-token"
$env:FCM_PROJECT_ID = ""
$env:FCM_SERVICE_ACCOUNT_JSON = ""
go run .\cmd\server serve
```

bash:

```bash
export DATABASE_URL="postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
export ADMIN_TOKEN="dev-admin-token"
unset FCM_PROJECT_ID FCM_SERVICE_ACCOUNT_JSON
go run ./cmd/server serve
```

Migrations run on start. Expected in the log (one JSON line per event): `push disabled: no FCM configuration, requests are stored but no phone is notified`, then `listening` with `"addr":":8080"` and `"push_enabled":false`. Leave this terminal open; it shows one line per call.

Push disabled: with both FCM variables empty the backend stores, waits on and answers requests exactly as in production, it only sends nothing. The phone's part is done with curl below. With real FCM values instead, the pushes to the fake device tokens registered in 5.2 are rejected by FCM and logged as `push: send failed`; the walkthrough is otherwise unchanged.

### 3.3 Optional: the container instead of `go run`

To check the image rather than the source:

```powershell
docker build -f Dockerfile -t ssh-sentinel-backend ..    # context: the repository root, the image embeds web/
docker run --rm -d --name ssh-sentinel-api -p 8080:8080 `
  -e DATABASE_URL="postgres://postgres:dev@host.docker.internal:5432/postgres?sslmode=disable" `
  -e ADMIN_TOKEN="dev-admin-token" ssh-sentinel-backend
docker logs ssh-sentinel-api
docker inspect --format "{{.State.Health.Status}}" ssh-sentinel-api    # healthy after the 10 s start period
```

`host.docker.internal` is how a container reaches the PostgreSQL published on the host with Docker Desktop; on a Linux host use the host's IP or put both containers on one network (that is what `infra/docker-compose/` does). `docker stop ssh-sentinel-api` at the end.

### 3.4 Optional: geo lookup stub

The geo scenario (5.12) needs a lookup endpoint. The geo client replaces `{ip}` in `GEO_LOOKUP_URL` with the source IP and expects a JSON body with `country`, `city` and `asn`; a static file server is enough. PowerShell:

```powershell
New-Item -ItemType Directory -Force "$env:TEMP\geo-stub" | Out-Null
Set-Content -Path "$env:TEMP\geo-stub\203.0.113.42.json" -Value '{"country":"KP","city":"Pyongyang","asn":"AS131279"}'
python -m http.server 9000 --directory "$env:TEMP\geo-stub"
```

bash:

```bash
mkdir -p /tmp/geo-stub
echo '{"country":"KP","city":"Pyongyang","asn":"AS131279"}' > /tmp/geo-stub/203.0.113.42.json
python3 -m http.server 9000 --directory /tmp/geo-stub
```

Then, before starting the backend, `$env:GEO_LOOKUP_URL = "http://127.0.0.1:9000/{ip}.json"` (bash: `export GEO_LOOKUP_URL="http://127.0.0.1:9000/{ip}.json"`). Any other IP gets a 404 from the stub, which the client treats as unknown. Without the stub every request has `"geo": null` and 5.12 only covers the rule management routes.

## 4. Enrol two servers, set the variables, define the helpers

Open the shell the curl part runs in (Git Bash on Windows) and go to `backend`. Enrol a Linux server and a Windows server: each `enroll` prints its token once on stdout and its messages on stderr, so the token is captured cleanly.

```bash
cd backend
export DATABASE_URL="postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
SERVER_TOKEN=$(go run ./cmd/server enroll --name web-01 --os linux)
WIN_TOKEN=$(go run ./cmd/server enroll --name win-01 --os windows)
echo "$SERVER_TOKEN"
```

Expected on stderr: `server "web-01" enrolled (id ...). The token above is shown once; put it in the agent's config file.` Enrolling the same name again answers `enroll: a server named "web-01" is already enrolled ...` and exits 1.

```bash
BASE=http://localhost:8080
ADMIN_TOKEN=dev-admin-token
```

Helpers used by every scenario. `agent` posts an access request as `web-01` and blocks until the backend answers; `phone` calls any route with the admin token; both print the body then `HTTP <status>`. `pending` prints the id of the newest pending request: that is how the "phone" finds what to decide, since no push arrives.

```bash
agent() {   # agent '<json>'                 POST /access-request as web-01
  curl -s -w 'HTTP %{http_code}\n' -X POST "$BASE/access-request" \
    -H "Authorization: Bearer $SERVER_TOKEN" -H 'Content-Type: application/json' -d "$1"
}
phone() {   # phone METHOD /path ['<json>']   any route as the app
  curl -s -w 'HTTP %{http_code}\n' -X "$1" "$BASE$2" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' ${3:+-d "$3"}
}
pending() { # id of the newest pending request
  curl -s "$BASE/history?status=pending" -H "Authorization: Bearer $ADMIN_TOKEN" | jq -r '.items[0].id'
}
```

`POST /access-request` blocks until a verdict comes or 25 s have passed, so the agent call runs in the background with its answer sent to a file, the phone decides, then `wait` collects the agent's answer:

```bash
agent '<json>' > /tmp/agent.json &
sleep 1
REQ=$(pending)
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve", "device_id": "'$DEVICE_A'"}'
wait; cat /tmp/agent.json
```

The verdict body is the contract's JSON with the single quotes closed and reopened around each shell variable. A second terminal instead of `&` and `wait` works as well. When a capture line needs the body alone, `| head -n 1` drops the `HTTP` line before `jq`.

## 5. Scenarios

### 5.1 Health

```bash
curl -s -w 'HTTP %{http_code}\n' "$BASE/healthz"
```

```json
{"status":"ok","db":"ok"}
HTTP 200
```

No token. With PostgreSQL stopped it answers `{"status":"degraded","db":"error"}` with `HTTP 503`.

### 5.2 Register two phones

```bash
phone POST /devices '{"fcm_token": "fake-token-pixel-8", "platform": "android", "label": "Pixel 8"}'
```

```json
{"id":"0c9d1e2f-3a4b-4c5d-8e6f-7a8b9c0d1e2f","platform":"android","label":"Pixel 8","created_at":"2026-09-14T20:00:00Z","last_seen_at":"2026-09-14T20:00:00Z"}
HTTP 201
```

The same call again answers `HTTP 200` with the same id: the route is an upsert on `fcm_token`, which is what the app relies on when FCM rotates the token. The FCM token itself is never returned. Register both phones and keep the ids:

```bash
DEVICE_A=$(phone POST /devices '{"fcm_token": "fake-token-pixel-8", "platform": "android", "label": "Pixel 8"}' | head -n 1 | jq -r .id)
DEVICE_B=$(phone POST /devices '{"fcm_token": "fake-token-pixel-7", "platform": "android", "label": "Pixel 7"}' | head -n 1 | jq -r .id)
phone GET /devices
```

```json
{"items":[{"id":"...","platform":"android","label":"Pixel 8","created_at":"...","last_seen_at":"..."},{"id":"...","platform":"android","label":"Pixel 7","created_at":"...","last_seen_at":"..."}]}
HTTP 200
```

### 5.3 SSH login approved by the phone

The agent, in the background:

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}' > /tmp/agent.json &
sleep 1
REQ=$(pending); echo "$REQ"
```

The backend log shows the request stored as `pending`. The phone approves:

```bash
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve", "device_id": "'$DEVICE_A'"}'
```

```json
{"request_id":"<REQ>","status":"approved","decided_at":"2026-09-14T20:12:07Z","decided_by_device":"Pixel 8","auto_blocked":null}
HTTP 200
```

The agent's answer:

```bash
wait; cat /tmp/agent.json
```

```json
{"request_id":"<REQ>","verdict":"approve","reason":"admin","decided_at":"2026-09-14T20:12:07Z"}
HTTP 200
```

Check `phone GET "/history?limit=1"`: the item has `"status":"approved"`, `"decided_by":"admin"`, `"decided_by_device":"Pixel 8"`, `"server":"web-01"`, and `"geo":{"country":"KP","city":"Pyongyang","asn":"AS131279"}` with the stub of 3.4, `"geo":null` without.

### 5.4 Denied

From another IP: admin denials count toward the auto-block of 5.11, three per IP per hour, and `203.0.113.42` is kept for that scenario.

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "198.51.100.7", "hostname": "web-01", "tty": "ssh", "command": null}' > /tmp/agent.json &
sleep 1; REQ=$(pending)
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "deny", "device_id": "'$DEVICE_B'"}'
wait; cat /tmp/agent.json
```

Phone:

```json
{"request_id":"<REQ>","status":"denied","decided_at":"2026-09-14T20:13:02Z","decided_by_device":"Pixel 7","auto_blocked":null}
HTTP 200
```

Agent:

```json
{"request_id":"<REQ>","verdict":"deny","reason":"admin","decided_at":"2026-09-14T20:13:02Z"}
HTTP 200
```

History: `"status":"denied"`, `"decided_by":"admin"`, `"decided_by_device":"Pixel 7"`.

### 5.5 Always allow with a TTL, then the whitelist answers alone

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}' > /tmp/agent.json &
sleep 1; REQ=$(pending)
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve_always", "ttl_seconds": 3600, "device_id": "'$DEVICE_A'"}'
```

```json
{"request_id":"<REQ>","status":"approved","decided_at":"2026-09-14T20:15:00Z","decided_by_device":"Pixel 8","whitelist_entry":{"id":"9a7d3e10-6b2f-4c55-8e0a-1f2b3c4d5e6f","username":"deploy","context":"ssh","server":"web-01","expires_at":"2026-09-14T21:15:00Z"},"auto_blocked":null}
HTTP 200
```

`whitelist_entry` is only present for `approve_always`; `expires_at` is one hour later (`ttl_seconds` absent or null would make it permanent). `wait; cat /tmp/agent.json` gives `"verdict":"approve","reason":"admin"` as in 5.3.

The same request again is answered at once, without a push and without a pending row:

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}'
```

```json
{"request_id":"...","verdict":"approve","reason":"whitelist","decided_at":"2026-09-14T20:15:20Z"}
HTTP 200
```

`phone GET "/history?status=pending"` answers `{"items":[]}`; `phone GET "/history?limit=1"` shows `"status":"whitelisted"`, `"decided_by":"whitelist"`, `"decided_by_device":null`.

### 5.6 sudo: entries are per context

`deploy` is whitelisted for ssh only, so its sudo is still pushed. `source_ip` is null (PAM gives sudo no remote host) and `command` is what the agent could read.

```bash
agent '{"context": "sudo", "mode": "enforce", "username": "deploy", "source_ip": null, "hostname": "web-01", "tty": "pts/0", "command": "sudo systemctl restart nginx"}' > /tmp/agent.json &
sleep 1; REQ=$(pending); echo "$REQ"
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve", "device_id": "'$DEVICE_A'"}'
wait; cat /tmp/agent.json
```

`REQ` is a pending id (not empty), the phone gets `"status":"approved"`, the agent `"verdict":"approve","reason":"admin"`. The history item:

```json
{"id":"<REQ>","server":"web-01","hostname":"web-01","context":"sudo","username":"deploy","source_ip":null,"tty":"pts/0","command":"sudo systemctl restart nginx","geo":null,"status":"approved","decided_by":"admin","decided_by_device":"Pixel 8","created_at":"...","expires_at":"...","decided_at":"..."}
```

### 5.7 Whitelist routes

As the agent, with the server token: the entries of `web-01` plus the global ones.

```bash
curl -s -w 'HTTP %{http_code}\n' "$BASE/whitelist" -H "Authorization: Bearer $SERVER_TOKEN"
```

```json
{"items":[{"id":"9a7d3e10-6b2f-4c55-8e0a-1f2b3c4d5e6f","username":"deploy","context":"ssh","server":"web-01","expires_at":"2026-09-14T21:15:00Z","created_at":"2026-09-14T20:15:00Z","created_from_request":"<REQ of 5.5>","created_by_device":"Pixel 8"}]}
HTTP 200
```

As the phone (`phone GET /whitelist`): every entry, the same list for now. Add a global entry, permanent:

```bash
phone POST /whitelist '{"username": "ansible", "context": "ssh", "server": null, "ttl_seconds": null}'
```

```json
{"id":"c1d2e3f4-0000-4aaa-8bbb-ccccdddd1111","username":"ansible","context":"ssh","server":null,"expires_at":null,"created_at":"2026-09-14T20:16:00Z","created_from_request":null,"created_by_device":null}
HTTP 201
```

The same call again answers `HTTP 200` with the same entry (existing entry refreshed). With `"server": "no-such-server"` it answers `HTTP 404`. Now the two agents see different lists: `web-01` (`$SERVER_TOKEN`) gets `deploy` and `ansible`, `win-01` (`$WIN_TOKEN`) gets `ansible` only.

Remove both, so that `deploy` is asked again in the scenarios below:

```bash
WL_DEPLOY=$(phone GET /whitelist | head -n 1 | jq -r '.items[] | select(.username == "deploy") | .id')
WL_ANSIBLE=$(phone GET /whitelist | head -n 1 | jq -r '.items[] | select(.username == "ansible") | .id')
phone DELETE "/whitelist/$WL_DEPLOY"
phone DELETE "/whitelist/$WL_ANSIBLE"
```

`HTTP 204` with an empty body, each. Deleting the same id again answers `{"error":"not found"}` with `HTTP 404`. `phone GET /whitelist` answers `{"items":[]}`.

### 5.8 Two phones answer: the first verdict wins

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "198.51.100.7", "hostname": "web-01", "tty": "ssh", "command": null}' > /tmp/agent.json &
sleep 1; REQ=$(pending)
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve", "device_id": "'$DEVICE_A'"}'
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "deny", "device_id": "'$DEVICE_B'"}'
wait; cat /tmp/agent.json
```

First verdict: `HTTP 200`, `"status":"approved"`. Second:

```json
{"error":"request already decided","status":"approved","decided_at":"2026-09-14T20:17:00Z","decided_by_device":"Pixel 8"}
HTTP 409
```

The agent gets the first verdict, `"verdict":"approve","reason":"admin"`. With push enabled, Pixel 7 would also receive a `request_decided` message; the `409` carries the same information.

### 5.9 Nobody answers

Run the agent in the foreground and wait:

```bash
time agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "198.51.100.7", "hostname": "web-01", "tty": "ssh", "command": null}'
```

After 25 s (`VERDICT_WAIT_SECONDS`):

```json
{"request_id":"...","verdict":"deny","reason":"timeout","decided_at":null}
HTTP 200
```

History: `"status":"timeout"`, `"decided_by":"timeout"`, `"decided_at":null`. A verdict that arrives after that, as a phone that got the push late would send:

```bash
REQ=$(phone GET "/history?status=timeout&limit=1" | head -n 1 | jq -r '.items[0].id')
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve", "device_id": "'$DEVICE_A'"}'
```

```json
{"error":"request already decided","status":"timeout","decided_at":null,"decided_by_device":null}
HTTP 409
```

Timeouts never count toward the auto-block: a phone left in a drawer blocks nobody.

### 5.10 Notify mode

Windows servers always send it; a Linux server can be installed with it during rollout. Nothing waits and nothing is denied. As `win-01`:

```bash
curl -s -w 'HTTP %{http_code}\n' -X POST "$BASE/access-request" \
  -H "Authorization: Bearer $WIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"context": "ssh", "mode": "notify", "username": "Administrator", "source_ip": "198.51.100.20", "hostname": "win-01", "tty": null, "command": null}'
```

```json
{"request_id":"...","verdict":"approve","reason":"notify","decided_at":"2026-09-14T20:20:00Z"}
HTTP 200
```

Immediate. History: `"server":"win-01"`, `"status":"notified"`, `"decided_by":null`. With push enabled the phones get an `access_notice` message, shown without buttons. `mode` absent means `enforce`.

### 5.11 Auto-block

Three admin denials from one IP inside an hour block it (`AUTOBLOCK_THRESHOLD`, `AUTOBLOCK_WINDOW_SECONDS`). `203.0.113.42` has no denial yet. Deny it three times:

```bash
for i in 1 2 3; do
  agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}' > /tmp/agent.json &
  sleep 1; REQ=$(pending)
  phone POST /verdict '{"request_id": "'$REQ'", "verdict": "deny", "device_id": "'$DEVICE_A'"}'
  wait
done
```

The first two verdict answers have `"auto_blocked":null`. The third:

```json
{"request_id":"...","status":"denied","decided_at":"2026-09-14T20:21:30Z","decided_by_device":"Pixel 8","auto_blocked":{"id":"2b3c4d5e-6f70-4a81-9b92-a3b4c5d6e7f8","ip":"203.0.113.42","denial_count":3}}
HTTP 200
```

The next request from that IP is denied at once, without a push and without a pending row:

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}'
```

```json
{"request_id":"...","verdict":"deny","reason":"blocked_ip","decided_at":"2026-09-14T20:21:45Z"}
HTTP 200
```

History: `"status":"blocked_ip"`, `"decided_by":"autoblock"`. The blocked IPs screen:

```bash
phone GET /blocked-ips
```

```json
{"items":[{"id":"2b3c4d5e-6f70-4a81-9b92-a3b4c5d6e7f8","ip":"203.0.113.42","reason":"autoblock","denial_count":3,"first_denied_at":"...","last_denied_at":"...","hit_count":1,"last_hit_at":"2026-09-14T20:21:45Z","created_at":"2026-09-14T20:21:30Z","expires_at":null}]}
HTTP 200
```

`hit_count` is 1: one request denied without a push since the block; `expires_at` null: until unblocked. Unblock:

```bash
BLOCK=$(phone GET /blocked-ips | head -n 1 | jq -r '.items[0].id')
phone DELETE "/blocked-ips/$BLOCK"
```

`HTTP 204`; `phone GET /blocked-ips` answers `{"items":[]}`. The next request from that IP is pushed again; approve it to close it:

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}' > /tmp/agent.json &
sleep 1; REQ=$(pending); echo "$REQ"
phone POST /verdict '{"request_id": "'$REQ'", "verdict": "approve", "device_id": "'$DEVICE_A'"}'
wait; cat /tmp/agent.json
```

`REQ` is a pending id again and the agent gets `"reason":"admin"`. The denials before the unblock are not counted again: three new ones are needed to block the IP a second time.

### 5.12 Geo rules

```bash
phone POST /geo-rules '{"country": "kp", "note": "smoke test"}'
```

```json
{"id":"7e8f9a0b-1c2d-4e3f-8a4b-5c6d7e8f9a0b","country":"KP","note":"smoke test","created_at":"2026-09-14T20:22:00Z"}
HTTP 201
```

Lower case in, upper case stored. The same call again answers `HTTP 409`; `{"country": "XX"}` answers `HTTP 400`; `phone GET /geo-rules` lists the rule.

With the stub of 3.4 and `GEO_LOOKUP_URL` set, `203.0.113.42` resolves to `KP` and is denied without a push:

```bash
agent '{"context": "ssh", "mode": "enforce", "username": "deploy", "source_ip": "203.0.113.42", "hostname": "web-01", "tty": "ssh", "command": null}'
```

```json
{"request_id":"...","verdict":"deny","reason":"blocked_geo","decided_at":"2026-09-14T20:22:30Z"}
HTTP 200
```

History: `"status":"blocked_geo"`, `"decided_by":"georule"`, `"geo":{"country":"KP","city":"Pyongyang","asn":"AS131279"}`. Without the stub the country is unknown, so the request is pushed and waits for a verdict (failure mode 10 of the architecture): decide it, or let it time out.

Remove the rule:

```bash
RULE=$(phone GET /geo-rules | head -n 1 | jq -r '.items[0].id')
phone DELETE "/geo-rules/$RULE"
```

`HTTP 204`.

### 5.13 History paging and filters

```bash
phone GET "/history?limit=2"
```

```json
{"items":[{"id":"...","server":"web-01","hostname":"web-01","context":"ssh","username":"deploy","source_ip":"203.0.113.42","tty":"ssh","command":null,"geo":null,"status":"blocked_geo","decided_by":"georule","decided_by_device":null,"created_at":"2026-09-14T20:22:30Z","expires_at":"2026-09-14T20:23:00Z","decided_at":"2026-09-14T20:22:30Z"},{"...":"the item before it"}],"next_before":"<created_at of the second item>"}
HTTP 200
```

Newest first. `next_before` is the `created_at` of the last item of the page; pass it back as `before`:

```bash
NEXT=$(phone GET "/history?limit=2" | head -n 1 | jq -r .next_before)
phone GET "/history?limit=2&before=$NEXT"
```

Older items. The last page has no `next_before`. Filters, combinable:

```bash
phone GET "/history?status=denied"
phone GET "/history?server=win-01"
phone GET "/history?username=deploy&context=sudo"
```

Each answers only matching items (`context=sudo` gives the one request of 5.6). `status=bogus` answers `HTTP 400`; `limit` above 200 is capped to 200.

### 5.14 Auth

```bash
curl -s -w 'HTTP %{http_code}\n' "$BASE/history"
curl -s -w 'HTTP %{http_code}\n' "$BASE/history" -H "Authorization: Bearer wrong-token"
curl -s -w 'HTTP %{http_code}\n' "$BASE/history" -H "Authorization: Bearer $SERVER_TOKEN"
curl -s -w 'HTTP %{http_code}\n' -X POST "$BASE/access-request" -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"context": "ssh", "username": "deploy", "hostname": "web-01"}'
```

In order: no token, wrong token, a server token on an app route, the admin token on the agent route.

```json
{"error":"missing or invalid token"}
HTTP 401
{"error":"missing or invalid token"}
HTTP 401
{"error":"admin token required"}
HTTP 403
{"error":"server token required"}
HTTP 403
```

An unknown route answers `{"error":"not found"}` with `HTTP 404`, a wrong method `HTTP 405`.

### 5.15 Remove a phone

```bash
phone DELETE "/devices/$DEVICE_B"
phone GET /devices
```

`HTTP 204`, then only Pixel 8 is listed. Deleting the id again answers `HTTP 404`. The contract says the history rows Pixel 7 decided keep its label: check `phone GET "/history?status=denied"` (5.4 was decided by Pixel 7).

## 6. Cleanup

Ctrl+C in the backend terminal: it logs `shutting down` then `stopped` (in-flight access requests get up to 30 s to finish). Ctrl+C the geo stub if it runs. Then:

```
docker stop ssh-sentinel-pg
```

The container was started with `--rm`: its data goes with it, and the next run starts from an empty database with the migrations applied again on start. Remove `bin\` if you built the binary by hand.
