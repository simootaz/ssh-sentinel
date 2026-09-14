# backend

Go API. Plain `net/http` handlers, one PostgreSQL database, FCM for push. Runs as one Docker container on any provider, or on Scaleway Serverless Functions through a thin adapter. Same code, same environment variables, same migrations everywhere.

## Role

Implements CONTRACT v1 (`docs/architecture.md`, section 5, frozen):

| Route group | Routes | Caller |
|---|---|---|
| access-request | `POST /access-request` | agent (server token) |
| verdict | `POST /verdict` | app (admin token) |
| history | `GET /history` | app |
| whitelist | `GET /whitelist`, `POST /whitelist`, `DELETE /whitelist/{id}` | app; agent for `GET` |
| devices | `GET /devices`, `POST /devices`, `DELETE /devices/{id}` | app |
| blocked-ips | `GET /blocked-ips`, `DELETE /blocked-ips/{id}` | app |
| geo-rules | `GET /geo-rules`, `POST /geo-rules`, `DELETE /geo-rules/{id}` | app |
| health | `GET /healthz` | operator, no auth |

On `POST /access-request` the backend authenticates the server, looks up the geo of the source IP, applies the rules in order (blocked IP, country blocklist, whitelist with expiry, notify mode) and only then stores a pending request, pushes it to every registered phone and waits for the first verdict.

Things to know:

- `access-request` and `verdict` may run in different processes: two function invocations on Scaleway, or two container replicas. They share nothing but the database. `access-request` re-reads the `requests` row about once per second until it is decided or the deadline passes. One code path for every deployment.
- The wait is capped at 25 s so that the agent's own 30 s budget always has margin. Function timeout on Scaleway is 60 s.
- First verdict wins: the `UPDATE` is conditional on `status = 'pending'`. A second verdict gets `409` with the winner's device label, and the other phones get a `request_decided` push.
- Auto-block: on each admin `deny`, the backend counts `denied` requests from the same IP inside the window. At the threshold it inserts the IP into `blocked_ips`; further requests from that IP are denied without a push until an admin unblocks it from the app.

## Layout

| Path | Content |
|---|---|
| `cmd/server/` | container entrypoint: config from env, opens the DB, serves the core handler on `LISTEN_ADDR`. `server migrate` applies `migrations/` |
| `internal/handler/` | the core: builds one `http.Handler` with every route (Go 1.22 `ServeMux` method and path patterns). Knows nothing about providers |
| `internal/rules/` | blocked IPs, geo rules, whitelist with expiry, auto-block counting |
| `internal/auth/` | bearer token checks (server tokens, admin token) |
| `internal/db/` | PostgreSQL access and queries, standard SQL only |
| `internal/fcm/` | FCM HTTP v1 client, high-priority data messages |
| `internal/geo/` | best-effort IP geolocation with a short timeout |
| `internal/model/` | request and response types shared by the handlers |
| `adapters/scaleway/api/` | Scaleway Serverless Functions adapter: exports `Handle(w, r)` and forwards to the core handler. One function serves every route, so clients get one base URL |
| `migrations/` | numbered SQL files applied in order (`001_init.sql`, ...) |
| `Dockerfile` | multi-stage build, static binary, minimal runtime image |

Adding a provider is a new folder under `adapters/`. Nothing under `internal/` changes.

## Configuration

Environment variables only. In the container they come from `.env` (compose) or the orchestrator; on Scaleway from Secret Manager through Terraform. Nothing is read from files, nothing is committed.

| Variable | Purpose | Default |
|---|---|---|
| `DATABASE_URL` | PostgreSQL connection string, any PostgreSQL 14+ | required |
| `ADMIN_TOKEN` | bearer token accepted from the phones | required |
| `FCM_PROJECT_ID` | Firebase project id | required |
| `FCM_SERVICE_ACCOUNT_JSON` | Firebase service account key, used to mint FCM access tokens | required |
| `LISTEN_ADDR` | container only, address to serve on | `:8080` |
| `VERDICT_WAIT_SECONDS` | long-poll cap | `25` |
| `AUTOBLOCK_THRESHOLD` | denials from one IP that trigger a block | `3` |
| `AUTOBLOCK_WINDOW_SECONDS` | window for counting those denials | `3600` |
| `AUTOBLOCK_DURATION_SECONDS` | how long a block lasts, `0` = until unblocked from the app | `0` |
| `GEO_LOOKUP_URL` | geolocation lookup endpoint; empty disables geo, and geo rules then never match | empty |

## Build

Go 1.22 or newer. From `backend\` (PowerShell):

```powershell
go mod tidy
go vet ./...
go build ./...
go build -o bin\server.exe .\cmd\server
docker build -t ssh-sentinel-backend .
```

The Scaleway zip is built by Terraform (`infra/scaleway/`) from `adapters/scaleway/api` with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`. You do not build it by hand.

## Run locally

```powershell
cd ..\infra\docker-compose
copy .env.example .env      # then fill in ADMIN_TOKEN and the FCM values
docker compose up
```

Backend on `http://localhost:8080`, PostgreSQL on `localhost:5432`, migrations applied on start. `curl http://localhost:8080/healthz` answers `{"status":"ok","db":"ok"}`.

## Test

```powershell
go test ./...
```

- Handler and rules tests use `httptest`, a fake FCM client and a fake clock. No network needed.
- Database tests need a PostgreSQL. Simplest is the compose stack above, or a bare container:

```powershell
docker run --rm -d --name ssh-sentinel-pg -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:16
$env:TEST_DATABASE_URL = "postgres://postgres:dev@localhost:5432/postgres?sslmode=disable"
go test ./...
docker stop ssh-sentinel-pg
```

  Tests that need the database skip themselves when `TEST_DATABASE_URL` is unset. Run them against PostgreSQL 14 as well before a release: 14 is the floor the contract promises.

- Smoke test after a deployment: call the routes with `curl` (or `Invoke-RestMethod`) using a dev server token and the dev admin token. Payloads are in `docs/architecture.md`, section 5.
