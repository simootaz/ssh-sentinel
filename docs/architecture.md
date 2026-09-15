# ssh-sentinel architecture

Status: design accepted on 2026-09-14, one version (the earlier v1/v2 split is merged), released as 1.0.0 on 2026-09-15. Section 5 is the API contract: v1, frozen, is what 1.0.0 implements; the v1.1 additions of 2026-09-15 (per-admin accounts and token rotation, session recording, web dashboard) are marked as such and are additive only, so a v1 client keeps working against a v1.1 backend. The rest of this document explains the decisions. Section 8 records what was decided and what is still to confirm.

Contents

1. Overview
2. End-to-end flow
3. Components
4. Database schema
5. API contract (CONTRACT v1 — FROZEN, plus the v1.1 additions)
6. Auth model
7. Failure modes
8. Design notes and decisions

## 1. Overview

ssh-sentinel adds a human step to SSH logins and to sudo. When someone logs in to a protected server, or runs sudo on it, the admins get a push notification on their phones showing who, from where, on which host, and for sudo the command. Any admin approves or denies from the notification. No answer within 30 seconds means the action is refused.

Targets: Linux servers (Ubuntu, Debian, AlmaLinux, Rocky, RHEL) with enforcement, Windows servers notify-only. macOS is best effort, later; nothing depends on it.

Components (decided):

| Component | Tech | Runs on | Job |
|---|---|---|---|
| agent | Go, one static binary | every protected server | PAM hook for ssh and sudo, asks the backend for a verdict, applies it |
| backend | Go, standard `net/http` handlers | any provider: one Docker container, or Scaleway Serverless Functions through a thin adapter | applies the rules, stores requests, sends pushes, brokers verdicts |
| database | any PostgreSQL 14+ | next to the backend (compose) or managed (Scaleway Serverless SQL) | requests, whitelist, devices, servers, blocked_ips, geo_rules; v1.1: admins, recordings |
| infra | Terraform for Scaleway (reference deployment), docker-compose (universal) | dev machine | creates or runs the backend and its database |
| mobile | Flutter, Android first | admin phones, several | shows requests, sends verdicts, manages whitelist, blocked IPs and geo rules; v1.1: admins, recording badge |
| web (v1.1) | single-page app, no build chain, embedded in the backend binary with `go:embed` | served by the backend at `/dashboard` | live pending requests, history, whitelist, blocked IPs, geo rules, admins, servers, recording playback |

v1.1 also adds session recording: on an approved SSH login in enforce mode the agent can wrap the session in `script(1)` and upload the typescript to the backend, where it is kept with the request (sections 3.1, 3.2, 5).

Push delivery is FCM on every provider.

## 2. End-to-end flow

```
SSH client       server: sshd/sudo + agent     backend                       FCM       admin phones: app
    |                    |                             |                       |               |
    |  ssh user@host,    |                             |                       |               |
    |  or sudo <cmd>     |                             |                       |               |
    |------------------->|                             |                       |               |
    |                    | PAM account phase           |                       |               |
    |                    | pam_exec -> ssh-sentinel    |                       |               |
    |                    |   check [--context=sudo]    |                       |               |
    |                    |                             |                       |               |
    |                    | [1] user in break-glass     |                       |               |
    |                    |     file?  yes -> exit 0    |                       |               |
    |                    |     (allow, no network)     |                       |               |
    |                    |                             |                       |               |
    |                    | [2] POST /access-request    |                       |               |
    |                    |     bearer: server token    |                       |               |
    |                    |---------------------------->|                       |               |
    |                    |                             | [3] token -> server   |               |
    |                    |                             | [4] geo lookup        |               |
    |                    |                             |     (best effort)     |               |
    |                    |                             | [5] rules, no push:   |               |
    |                    |                             |     blocked IP?  deny |               |
    |                    |                             |     geo rule?    deny |               |
    |                    |                             |     whitelist?  allow |               |
    |                    |                             |     notify mode? allow|               |
    |                    |                             | [6] INSERT requests   |               |
    |                    |                             |     status = pending  |               |
    |                    |                             |     expires = now+30s |               |
    |                    |                             | [7] push, high prio,  |               |
    |                    |                             |     to every device   |               |
    |                    |                             |---------------------->|-------------->|
    |                    | agent blocks, 30 s max      | [8] poll requests row |               | request screen, or
    |                    |                             |     every ~1 s,       |               | notification with
    |                    |                             |     25 s max          |               | 3 action buttons,
    |                    |                             |                       |               | 30 s countdown
    |                    |                             |                       |               |
    |                    |                             | [9] POST /verdict     |               |
    |                    |                             |     bearer: admin tok |               |
    |                    |                             |<--------------------------------------|
    |                    |                             | [10] first verdict    |               |
    |                    |                             |      wins, others 409 |               |
    |                    |                             |      approve_always:  |               |
    |                    |                             |      + whitelist, TTL |               |
    |                    |                             |      deny: count for  |               |
    |                    |                             |      auto-block       |               |
    |                    |                             |      request_decided  |               |
    |                    |                             |      push to the rest |               |
    |                    |                             |---------------------->|-------------->|
    |                    | [11] 200 {verdict, reason}  |                       |               |
    |                    |<----------------------------|                       |               |
    |                    | [12] exit 0 = allow         |                       |               |
    |                    |      exit 1 = deny          |                       |               |
    |  session opens or  |                             |                       |               |
    |  command runs,     |                             |                       |               |
    |  or refused        |                             |                       |               |
    |<-------------------|                             |                       |               |
```

Step by step:

1. sshd verifies the credentials (password or public key), then runs the PAM `account` stack of `/etc/pam.d/sshd`, where `pam_exec` runs `ssh-sentinel check`. For sudo the same line sits in `/etc/pam.d/sudo` with `--context=sudo`. `pam_exec` passes `PAM_USER`, `PAM_RHOST` (empty for sudo), `PAM_SERVICE` and `PAM_TTY`. The agent reads `/etc/ssh-sentinel/breakglass` first; a match ends here with exit 0 and nothing leaves the server. Break-glass covers both contexts.
2. The agent sends `POST /access-request` with the context, username, source IP (null for sudo), hostname, tty, the sudo command when it could read it, and its mode. The server's bearer token authenticates the call. The whole budget is 30 s, connection included.
3. The backend hashes the token and finds the server row. Unknown token: 401, the agent denies.
4. Geo lookup on the source IP, short timeout. A failure leaves `geo` empty; the request goes on.
5. Rules, in this order, each one ending the request without a push: the source IP has an active row in `blocked_ips` (status `blocked_ip`, deny); the country matches a row in `geo_rules` (status `blocked_geo`, deny); an unexpired whitelist entry matches `(username, context)` for this server or for every server (status `whitelisted`, approve); the request is in notify mode (status `notified`, approve, informational push).
6. Otherwise it inserts a `pending` row with `expires_at = now() + 30 s`.
7. One FCM high-priority data message per row in `devices`, carrying the request id, context, user, IP, geo, host, command and `expires_at`.
8. The function re-reads the row about once per second until `status` leaves `pending` or 25 s have passed. Separate invocations, or container replicas, share only the database, so this is how `access-request` learns about the verdict.
9. Any admin taps a button. The app, or its background handler when the app is closed, calls `POST /verdict` with the admin token and its device id.
10. First verdict wins: the update is conditional on `status = 'pending'`, a later verdict gets `409`. `approve_always` also inserts or refreshes a whitelist entry for `(username, context, server)` with the optional TTL. `deny` counts recent denials from that IP and inserts a `blocked_ips` row at the threshold. A `request_decided` push goes to the other phones so they can dismiss the notification.
11. `access-request` returns the verdict to the agent. If nothing came in 25 s, it marks the row `timeout` and returns `deny`.
12. Exit 0 lets sshd open the session or sudo run the command; exit 1 makes PAM refuse.

Side path, whitelist sync: `ssh-sentinel sync` runs from a timer and calls `GET /whitelist` with the server token. The backend returns the unexpired entries for that server plus the global ones, with `context` and `expires_at`. The agent writes them to `/var/lib/ssh-sentinel/whitelist.json`. The cache is only read when the backend cannot be reached, and the agent skips entries whose `expires_at` has passed by the server's clock.

Side path, Windows: `ssh-sentinel watch` reads the event log and calls `POST /access-request` with `mode` set to `notify`. The backend stores the request as `notified`, sends an informational push, and answers immediately. Nothing waits. A Linux server can run in the same mode during rollout.

Side path, phones: at first launch, and whenever FCM rotates its token, the app calls `POST /devices`. The returned device id goes into every `POST /verdict` so the history shows which phone decided.

Side path, admins (v1.1): the token a phone or the dashboard sends is either a per-admin token, created with `POST /admins` and shown once, or the bootstrap `ADMIN_TOKEN` from the backend's environment. The backend maps it to an admin identity, and the history shows `decided_by_admin` next to `decided_by_device`. Tokens are rotated with `POST /admins/{id}/rotate` and `POST /servers/{id}/rotate`; the new token is returned once, the old one stops working at once.

Side path, session recording (v1.1): when `record_sessions` is on in the agent config and the backend approved an SSH login in enforce mode (reason `admin` or `whitelist`), step 12 also leaves a ticket for that login. sshd then runs the agent's session wrapper as the user (`ForceCommand`), which finds the ticket and runs the shell, or the requested command, under `script -q -f -a` writing `/var/lib/ssh-sentinel/recordings/<request-id>.log`. When the session ends the wrapper hands the file to `ssh-sentinel upload`, which gzips it and sends it to `POST /recordings/{request_id}` with the server token. `sync` retries what could not be uploaded. Details and limits: section 3.1, failure modes in section 7.

## 3. Components

### 3.1 agent

Subcommands: `check [--context=ssh|sudo]` (PAM hook), `sync` (whitelist cache refresh; v1.1: also retries pending recording uploads), `watch` (Windows event log), `version`; v1.1: `session` (the sshd `ForceCommand` wrapper that records) and `upload` (sends one recording). The config holds the backend base URL, the server token and the mode (`enforce` or `notify`); v1.1 adds `record_sessions` (default `false`), `record_max_total_mb` (default `500`) and `record_max_upload_mb` (default `20`).

Session recording (v1.1), Linux only, SSH only, enforce mode only:

- Hook: with `record_sessions` on, `install.sh` writes `/etc/ssh/sshd_config.d/ssh-sentinel.conf` with `ForceCommand /usr/local/bin/ssh-sentinel session` and reloads sshd (distros without the drop-in directory get an `Include` line). The wrapper runs as the logged-in user, which is what sshd gives a `ForceCommand`.
- Ticket: `check`, running as root in the PAM `account` phase, has the request id. When the backend answered `approve` with reason `admin` or `whitelist`, it pre-creates `/var/lib/ssh-sentinel/recordings/<request-id>.log` owned by the user, mode 0600, with the append-only attribute (`chattr +a`, ext4 and xfs), and writes a ticket `/var/lib/ssh-sentinel/tickets/<user>` (owned by the user, 0400, directory root 0711) holding the request id. Break-glass logins, cache-approved logins during an outage and notify-mode logins get no ticket: there is no backend request id, or nothing was enforced.
- Wrapper: `session` reads and deletes its ticket, then runs `script -q -f -a <file> -c <command>` where the command is `$SSH_ORIGINAL_COMMAND` when set, the user's login shell otherwise. No ticket, no tty, or an sftp or scp subsystem request: the command is executed directly, unrecorded. `script` missing: the command is executed unrecorded and a syslog line says so. A recording problem never refuses a session that the backend approved; the approval is the security boundary, the recording is the audit trail.
- Tamper resistance: the file belongs to the user's own session, so `script` can append to it but the append-only attribute stops the user from truncating or editing it; only root can clear the attribute. It is an audit aid, not evidence. A privileged recorder would be the tamper-proof follow-up, out of scope.
- Upload: the wrapper is the user's process and has no server token, so it never talks to the backend. It marks the end of the session by removing the `<request-id>.open` marker that `check` created next to the file. The root-owned `upload` step, run by the `sync` timer every 5 minutes and by a `systemd` path unit when a marker disappears, sends every closed, not yet uploaded recording: gzip body, `POST /recordings/{request_id}`, server token. After a `201` the file is marked `.uploaded` and stays local; `413` (over the backend cap) sends the first `record_max_upload_mb` MiB again with `truncated=true`; `501` (deployment without recording storage, Scaleway today) marks the file `.rejected` and stops retrying; anything else is retried on the next `sync`.
- Rotation: before each recording starts and after each `sync`, the agent deletes the oldest recordings, uploaded or not, until the directory is under `record_max_total_mb`, and logs each deletion. Uploaded files go first.
- Format: a raw `script` typescript, terminal escape sequences included. No timing file in v1.1; the dashboard shows it as plain text with the escape sequences stripped.

Decision table for `check`, evaluated top to bottom:

| # | Condition | Result | Network used |
|---|---|---|---|
| 1 | username in `/etc/ssh-sentinel/breakglass` | allow | none |
| 2 | mode is `notify` | allow, request sent as best effort | attempted |
| 3 | backend answers 200 with `verdict = approve` (an admin approved, or a rule allowed) | allow | yes |
| 4 | backend answers 200 with `verdict = deny` (an admin denied, nobody answered, or a rule denied) | deny | yes |
| 5 | backend answers 4xx (bad token, bad payload) | deny | yes |
| 6 | backend unreachable (DNS, connect, TLS, 5xx, or no answer within 30 s) and the cache holds an unexpired entry for `(username, context)` | allow | attempted |
| 7 | backend unreachable and no such entry | deny | attempted |
| 8 | anything unexpected (panic, unreadable files) | deny | n/a |

Row 6 is what "backend unreachable + unknown user = deny" implies for known users: a user an admin already marked "always allow" keeps working during a backend outage, from the local copy, until the entry expires. Row 5 is deliberately not treated as an outage: a backend that answers 401 has revoked this server, and the cache must not override that.

Every decision is logged as one syslog line of `key=value` pairs (`decision`, `context`, `user`, `rhost`, `host`, `reason`, `request`). The format is stable and documented in `agent/README.md`; the fail2ban filter shipped in `agent/scripts/fail2ban/` matches `decision=deny` lines to ban locally. That is a per-server layer; the backend's auto-block is the fleet-wide layer.

Files on the server, config format and build instructions: `agent/README.md`.

### 3.2 backend

One Go module. `internal/handler` builds a single `http.Handler` with every route of the contract; it knows nothing about the provider. Two entry points use it:

- `cmd/server`: the container. Reads the environment, opens the database, applies migrations, serves on `LISTEN_ADDR`, answers `GET /healthz`. Runs anywhere Docker runs, next to any PostgreSQL 14+.
- `adapters/scaleway/api`: Scaleway Serverless Functions. Exports `Handle(w, r)` and forwards to the same handler. One function serves every route, so the base URL is one URL on Scaleway as it is with the container.

Adding a provider is a new folder under `adapters/`; the contract and the core do not change. Portable pieces only: standard `net/http`, standard SQL, FCM over HTTPS.

v1.1 additions, all in the core handler so both entry points get them:

- Admins (`internal/auth`): the admin token is looked up in `admins` by its hash, disabled rows excluded, then compared with `ADMIN_TOKEN` as the bootstrap admin. Every admin route, `POST /verdict` included, runs with an admin identity; the handlers write `decided_by_admin` and `created_by_admin`. `server admin add --name <name>` creates the first admin from the command line the way `enroll` creates a server, so a deployment can drop `ADMIN_TOKEN` once named admins exist.
- Recordings (`internal/recordings`): `POST /recordings/{request_id}` checks the server token owns the request, enforces `RECORDING_MAX_BYTES` (default 20 MiB, decompressed), stores the typescript as a file under `RECORDINGS_DIR` and a row in `recordings`. `GET /recordings/{request_id}` streams it back. Storage is a small interface with one implementation, local disk: on the container `RECORDINGS_DIR` is a volume (`recordings` in the compose file). A Scaleway function has no persistent disk, so with `RECORDINGS_DIR` empty the two routes answer `501 {"error": "recordings not supported on this deployment"}`, the agent keeps the files locally and stops retrying. Object Storage behind the same interface is the known limitation to lift; it is not part of v1.1.
- Dashboard (`web/`, section 3.6): the static files are compiled into the binary with `go:embed` and served at `/dashboard` by the same handler, so they exist on the container and on Scaleway alike, with no extra deployment step. The dashboard calls the API of section 5 with an admin token, nothing else.

Verdict wait: the `access-request` invocation that sent the push and the `verdict` invocation that receives the answer are different processes (two function instances, or two container replicas). They coordinate through the `requests` row only, polled once per second for at most 25 s. One code path for every deployment, and replicas need no sticky sessions.

Rules (`internal/rules`): blocked IPs, geo rules, whitelist with expiry, auto-block counting. Rules run before the whitelist, so a whitelisted user coming from a blocked IP or country is denied.

Timing budget: agent 30 s total, backend wait 25 s, function timeout 60 s. The backend always answers before the agent gives up, unless the network between them is very slow.

### 3.3 database

Any PostgreSQL 14 or newer. Standard features only: `gen_random_uuid()` (built in since 13), `INET`, `JSONB`, `TIMESTAMPTZ`, partial indexes, `CHECK` constraints. No extensions, no provider-specific SQL. Schema in section 4. Migrations are numbered SQL files in `backend/migrations/`, applied by `server migrate` from the same binary: the container runs it on start, the Scaleway deploy runs it as a step. `001` is the v1 schema, `002` adds `devices.deleted_at`; v1.1 adds `003_admins.sql` (the `admins` table, `requests.decided_by_admin`, `whitelist.created_by_admin`) and `004_recordings.sql`. Every v1.1 column is nullable and every v1.1 table is new, so a v1 backend binary still runs against a v1.1 schema and the other way round during a rolling upgrade.

### 3.4 infra

`infra/scaleway/`: Terraform, the reference deployment. Functions namespace, the `api` function, Serverless SQL, Secret Manager, the IAM application whose key is the database credential. Two environments, dev and prod, state in Object Storage.

`infra/docker-compose/`: the universal option. Backend container, PostgreSQL 16, optional Caddy for TLS. Any VM, any cloud, bare metal. Other providers are additional folders later. Details: `infra/README.md`.

### 3.5 mobile

Flutter, Android first. Six screens: incoming request, history, whitelist, blocked IPs, geo rules, settings. FCM high-priority data messages carry the request; the app builds the notification with the three action buttons and shows a countdown from `expires_at`, not from the time it received the push. Several phones register through `POST /devices`; the first verdict wins and the others see who decided. Always allow opens a TTL picker (1 h, 24 h, custom, permanent). Details: `mobile/README.md`.

v1.1: the token in Settings is a per-admin token or the bootstrap one, the app does not care which. Settings gets an Admins section: list, add (the new token is shown once, with a copy button, and never again), rotate (same), disable. History rows and the incoming request outcome show `decided_by_admin` next to the device label ("Approved by alice, Pixel 8"). A history row with a recording shows a badge; tapping it opens the recording as plain text from `GET /recordings/{request_id}`, escape sequences stripped, for a quick look. The dashboard is the place for longer reads.

### 3.6 web (v1.1)

A single-page app in `web/`: `index.html`, one stylesheet, plain JavaScript modules, no framework and no build step; a small dependency (an ANSI escape stripper, a date helper) is vendored as a file when needed. The backend embeds the folder with `go:embed` and serves it at `/dashboard`, so the dashboard is part of every deployment with nothing to host and no CORS: same origin as the API.

Login is the admin token, entered once and kept in the browser's session storage, sent as the bearer on every call. No cookies, so no CSRF surface; the API is what it always was. Screens: live pending requests (polled through `GET /history?status=pending` every 2 s, with the Deny / Approve / Always allow buttons, no push needed), history with filters and paging, whitelist, blocked IPs, geo rules, admins (list, add with the token shown once, rotate, disable), servers (list, rotate, revoke), recording playback as plain text. The backend sends a strict `Content-Security-Policy` for `/dashboard` (own origin only, no inline scripts).

The dashboard is a client of the contract like the app: anything it needs is a route in section 5, never a private endpoint.

## 4. Database schema

```sql
-- Servers running the agent. One row per server, one bearer token per row.
CREATE TABLE servers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL UNIQUE,              -- hostname or friendly label, shown in the app
    token_hash    TEXT NOT NULL UNIQUE,              -- sha256 hex of the bearer token, never the token
    os            TEXT NOT NULL CHECK (os IN ('linux', 'windows', 'darwin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ                        -- updated on every authenticated call
);

-- Phones that receive pushes. Every pending request is pushed to all of them.
CREATE TABLE devices (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fcm_token     TEXT NOT NULL UNIQUE,
    platform      TEXT NOT NULL CHECK (platform IN ('android', 'ios')),
    label         TEXT,                              -- "Pixel 8", free text, shown in history
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ                        -- DELETE /devices sets it; history keeps the label (migration 002)
);

-- v1.1. Admin accounts, one bearer token each. The bootstrap ADMIN_TOKEN from the
-- environment is not in this table; it acts as the admin named "bootstrap".
CREATE TABLE admins (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL UNIQUE,              -- "alice", shown in history
    token_hash    TEXT NOT NULL UNIQUE,              -- sha256 hex of the bearer token, never the token
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at   TIMESTAMPTZ,                       -- DELETE /admins sets it; the row stays, history references it
    last_seen_at  TIMESTAMPTZ                        -- updated on every authenticated call
);

-- One row per ssh login or sudo invocation that reached the backend.
CREATE TABLE requests (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id         UUID NOT NULL REFERENCES servers(id),
    context           TEXT NOT NULL DEFAULT 'ssh' CHECK (context IN ('ssh', 'sudo')),
    username          TEXT NOT NULL,
    source_ip         INET,                            -- NULL when unknown (sudo)
    hostname          TEXT NOT NULL,                   -- as reported by the agent, display only
    tty               TEXT,
    command           TEXT,                            -- sudo only, best effort
    geo               JSONB,                           -- {"country": "FR", "city": "Paris", "asn": "AS12876"}, may be NULL
    status            TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'approved', 'denied', 'timeout',
                                        'whitelisted', 'blocked_ip', 'blocked_geo', 'notified')),
    decided_by        TEXT                             -- NULL while pending, and for notified
                      CHECK (decided_by IN ('admin', 'whitelist', 'timeout', 'autoblock', 'georule')),
    decided_by_device UUID REFERENCES devices(id) ON DELETE SET NULL,
    decided_by_admin  UUID REFERENCES admins(id),    -- v1.1. NULL for the bootstrap admin and for rule decisions
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,            -- created_at + 30 s
    decided_at        TIMESTAMPTZ
);

CREATE INDEX requests_pending_idx   ON requests (status, expires_at) WHERE status = 'pending';
CREATE INDEX requests_history_idx   ON requests (created_at DESC);
CREATE INDEX requests_server_idx    ON requests (server_id, created_at DESC);
CREATE INDEX requests_denied_ip_idx ON requests (source_ip, decided_at) WHERE status = 'denied';

-- Always-allow list, per context. server_id NULL means every server. expires_at NULL means permanent.
CREATE TABLE whitelist (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username          TEXT NOT NULL,
    context           TEXT NOT NULL DEFAULT 'ssh' CHECK (context IN ('ssh', 'sudo')),
    server_id         UUID REFERENCES servers(id) ON DELETE CASCADE,
    expires_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_from      UUID REFERENCES requests(id),    -- the request approved with approve_always, if any
    created_by_device UUID REFERENCES devices(id) ON DELETE SET NULL,
    created_by_admin  UUID REFERENCES admins(id)     -- v1.1. NULL for the bootstrap admin
);

-- Two indexes instead of one UNIQUE (username, context, server_id): in PostgreSQL two NULLs
-- are not equal, so a plain UNIQUE would allow duplicate global entries.
CREATE UNIQUE INDEX whitelist_user_ctx_server_idx ON whitelist (username, context, server_id) WHERE server_id IS NOT NULL;
CREATE UNIQUE INDEX whitelist_user_ctx_global_idx ON whitelist (username, context) WHERE server_id IS NULL;

-- IPs blocked by the backend after repeated denials. Requests from them are denied without a push.
CREATE TABLE blocked_ips (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ip              INET NOT NULL UNIQUE,
    reason          TEXT NOT NULL DEFAULT 'autoblock',
    denial_count    INTEGER NOT NULL,                  -- denials that triggered the block
    first_denied_at TIMESTAMPTZ NOT NULL,
    last_denied_at  TIMESTAMPTZ NOT NULL,
    hit_count       INTEGER NOT NULL DEFAULT 0,        -- requests auto-denied since the block
    last_hit_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ                        -- NULL = until unblocked from the app
);

-- Country blocklist. Requests whose geo country matches are denied without a push.
CREATE TABLE geo_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    country     CHAR(2) NOT NULL UNIQUE,               -- ISO 3166-1 alpha-2, upper case
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- v1.1. One recording per approved SSH request, uploaded by the agent. The typescript
-- itself is a file under RECORDINGS_DIR (local disk on the container); this row is the
-- metadata. No row means no recording.
CREATE TABLE recordings (
    request_id   UUID PRIMARY KEY REFERENCES requests(id),
    server_id    UUID NOT NULL REFERENCES servers(id),
    storage      TEXT NOT NULL DEFAULT 'disk' CHECK (storage IN ('disk')),   -- 'object' later, for Scaleway
    path         TEXT NOT NULL,                      -- relative to the storage root: "<yyyy>/<mm>/<request_id>.log.gz"
    size_bytes   BIGINT NOT NULL,                    -- decompressed
    sha256       TEXT NOT NULL,                      -- of the decompressed typescript, for the download
    truncated    BOOLEAN NOT NULL DEFAULT false,     -- the agent cut it at its upload cap
    started_at   TIMESTAMPTZ NOT NULL,               -- session start, from the agent
    ended_at     TIMESTAMPTZ NOT NULL,               -- session end, from the agent
    uploaded_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX recordings_server_idx ON recordings (server_id, uploaded_at DESC);
```

Request status meanings:

| status | Set by | Meaning |
|---|---|---|
| `pending` | access-request | pushed, waiting for a verdict |
| `approved` | verdict | an admin tapped Approve or Always allow |
| `denied` | verdict | an admin tapped Deny; counts toward auto-block |
| `timeout` | access-request | no verdict within the wait window, the agent was told to deny |
| `whitelisted` | access-request | matched an unexpired whitelist entry, approved without a push |
| `blocked_ip` | access-request | source IP in `blocked_ips`, denied without a push |
| `blocked_geo` | access-request | country in `geo_rules`, denied without a push |
| `notified` | access-request | notify mode (Windows, or Linux in rollout), informational push, nothing waited |

Break-glass logins never reach the backend and have no row. They are logged in syslog on the server.

## 5. API contract (CONTRACT v1 — FROZEN, plus the v1.1 additions)

> CONTRACT v1 — FROZEN on 2026-09-14. The agent, the backend, the infra and the app are built in parallel against this section. Nothing in it changes without an explicit decision and a version bump: v1.x for additive changes (a new optional field, a new endpoint), v2 for anything a client could break on. Every side ignores unknown JSON fields, which is what makes additive changes safe.

> CONTRACT v1.1 — ADDITIVE, decided on 2026-09-15. Everything marked "v1.1" below is new on top of v1 and changes nothing a v1 client relies on: new routes, new optional response fields, one new optional query parameter. A 1.0.0 agent or app keeps working against a v1.1 backend. The v1.1 routes are grouped at the end of this section; the v1.1 fields on v1 routes are marked inline.

Routes:

| Route | Caller | Purpose |
|---|---|---|
| `POST /access-request` | agent | ask for a verdict, blocks until decided |
| `POST /verdict` | app | decide a pending request |
| `GET /history` | app | list requests |
| `GET /whitelist` | app, agent | list always-allow entries |
| `POST /whitelist` | app | add or refresh an entry, optional TTL |
| `DELETE /whitelist/{id}` | app | remove an entry |
| `POST /devices` | app | register or refresh this phone |
| `GET /devices` | app | list phones |
| `DELETE /devices/{id}` | app | remove a phone |
| `GET /blocked-ips` | app | list blocked IPs |
| `DELETE /blocked-ips/{id}` | app | unblock |
| `GET /geo-rules` | app | list country blocks |
| `POST /geo-rules` | app | add a country block |
| `DELETE /geo-rules/{id}` | app | remove a country block |
| `GET /healthz` | operator | liveness, no auth |
| `GET /admins` (v1.1) | app, dashboard | list admin accounts |
| `POST /admins` (v1.1) | app, dashboard | create an admin, returns its token once |
| `POST /admins/{id}/rotate` (v1.1) | app, dashboard | new token for an admin, returned once |
| `DELETE /admins/{id}` (v1.1) | app, dashboard | disable an admin, the row stays |
| `GET /servers` (v1.1) | app, dashboard | list enrolled servers |
| `POST /servers/{id}/rotate` (v1.1) | app, dashboard | new token for a server, returned once |
| `DELETE /servers/{id}` (v1.1) | app, dashboard | revoke a server, the row stays |
| `POST /recordings/{request_id}` (v1.1) | agent | upload the session recording of an approved request |
| `GET /recordings/{request_id}` (v1.1) | app, dashboard | download it |
| `GET /dashboard` (v1.1) | browser | the web dashboard, static files, no auth on the files themselves |

"app" means any admin-token client: the phone app and, from v1.1, the dashboard. Every admin-token route runs as an admin identity (section 6); v1.1 adds that identity to the responses without removing anything.

Conventions:

- One base URL per deployment, the routes above appended to it. Container: whatever the reverse proxy exposes. Scaleway: the `api` function URL from the Terraform output. Agents and phones are configured with that base URL and nothing else.
- HTTPS only. `Content-Type: application/json` in both directions. `Authorization: Bearer <token>` on every call except `/healthz`.
- Timestamps are RFC 3339 in UTC. Ids are UUIDs. IPs are plain text (`203.0.113.42`, IPv6 allowed).
- Errors: `{"error": "short message"}` with `400` (bad payload), `401` (missing or invalid token), `403` (token not allowed for this call), `404`, `409` (see each route), `500`. Some `409` bodies carry extra fields, listed with the route.
- List routes return `{"items": [...]}` and, when paged, `next_before`.

Enumerations:

| Field | Values |
|---|---|
| `context` | `ssh`, `sudo` |
| `mode` | `enforce`, `notify` |
| request `status` | `pending`, `approved`, `denied`, `timeout`, `whitelisted`, `blocked_ip`, `blocked_geo`, `notified` |
| `decided_by` | `admin`, `whitelist`, `timeout`, `autoblock`, `georule`, or null |
| `verdict` sent by the app | `approve`, `deny`, `approve_always` |
| `verdict` returned to the agent | `approve`, `deny` |
| `reason` returned to the agent | `admin`, `whitelist`, `timeout`, `blocked_ip`, `blocked_geo`, `notify` |
| push `type` | `access_request`, `access_notice`, `request_decided` |
| `decided_by_admin`, `created_by_admin` (v1.1) | an admin name, `bootstrap` for the environment token, or null when no admin was involved |
| recording `storage` (v1.1) | `disk` |

### POST /access-request

Caller: agent, server token. Blocks until the verdict is known or the wait window is over.

Request, an SSH login:

```json
{
  "context": "ssh",
  "mode": "enforce",
  "username": "deploy",
  "source_ip": "203.0.113.42",
  "hostname": "web-01",
  "tty": "ssh",
  "command": null
}
```

Request, a sudo invocation. `source_ip` is null because PAM gives sudo no remote host; `command` is filled when the agent could read it, null otherwise:

```json
{
  "context": "sudo",
  "mode": "enforce",
  "username": "deploy",
  "source_ip": null,
  "hostname": "web-01",
  "tty": "pts/0",
  "command": "sudo systemctl restart nginx"
}
```

`mode` defaults to `enforce` when absent. The server identity comes from the token, never from `hostname`, which is display only.

Response `200`:

```json
{
  "request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
  "verdict": "approve",
  "reason": "admin",
  "decided_at": "2026-09-14T20:12:07Z"
}
```

`reason` says why: `admin` (a button was tapped), `whitelist` (unexpired entry, no push), `notify` (notify mode, informational push), `timeout` (no answer, verdict is `deny`), `blocked_ip` or `blocked_geo` (rule, no push, verdict is `deny`). `decided_at` is null for `timeout`.

Agent rule: exit 0 only for HTTP 200 with `verdict = "approve"`, or in notify mode. Everything else follows the decision table in 3.1.

### POST /verdict

Caller: app, admin token.

Request:

```json
{
  "request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
  "verdict": "approve_always",
  "ttl_seconds": 3600,
  "device_id": "0c9d1e2f-3a4b-4c5d-8e6f-7a8b9c0d1e2f"
}
```

`ttl_seconds` is only read with `approve_always`: absent or null means permanent, otherwise the whitelist entry expires after that many seconds (the app offers 1 h, 24 h, custom). `device_id` is the id returned by `POST /devices`; optional, but without it the history cannot say which phone decided.

Response `200`:

```json
{
  "request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
  "status": "approved",
  "decided_at": "2026-09-14T20:12:07Z",
  "decided_by_device": "Pixel 8",
  "decided_by_admin": "alice",
  "whitelist_entry": {
    "id": "9a7d3e10-6b2f-4c55-8e0a-1f2b3c4d5e6f",
    "username": "deploy",
    "context": "ssh",
    "server": "web-01",
    "expires_at": "2026-09-14T21:12:07Z"
  },
  "auto_blocked": null
}
```

`whitelist_entry` is present only for `approve_always`. `auto_blocked` is filled only when this `deny` reached the threshold:

```json
{ "auto_blocked": { "id": "2b3c4d5e-6f70-4a81-9b92-a3b4c5d6e7f8", "ip": "203.0.113.42", "denial_count": 3 } }
```

Multi-admin: every phone gets the push, the first `POST /verdict` wins, every later one gets `409`:

```json
{
  "error": "request already decided",
  "status": "approved",
  "decided_at": "2026-09-14T20:12:07Z",
  "decided_by_device": "Pixel 8",
  "decided_by_admin": "alice"
}
```

The same `409` with `"status": "timeout"` means the request expired before anyone answered.

v1.1: `decided_by_admin` in the `200` and the `409` bodies is the name of the admin whose token sent the winning verdict, `bootstrap` for the environment token, null when the request was decided by a rule. `device_id` stays optional: the dashboard sends none, and the history then shows the admin name alone.

### GET /history

Caller: app, admin token.

Query parameters, all optional: `limit` (default 50, max 200), `before` (RFC 3339, requests created before this instant, for paging), `server` (server name), `username`, `context`, `status`.

Response `200`:

```json
{
  "items": [
    {
      "id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
      "server": "web-01",
      "hostname": "web-01",
      "context": "sudo",
      "username": "deploy",
      "source_ip": null,
      "tty": "pts/0",
      "command": "sudo systemctl restart nginx",
      "geo": null,
      "status": "approved",
      "decided_by": "admin",
      "decided_by_device": "Pixel 8",
      "decided_by_admin": "alice",
      "created_at": "2026-09-14T20:11:40Z",
      "expires_at": "2026-09-14T20:12:10Z",
      "decided_at": "2026-09-14T20:12:07Z",
      "recording": null
    }
  ],
  "next_before": "2026-09-14T20:11:40Z"
}
```

v1.1: `decided_by_admin` as in `POST /verdict`. `recording` is null when the request has no recording, otherwise `{"size_bytes": 48213, "truncated": false, "started_at": "...", "ended_at": "...", "uploaded_at": "..."}`; the content comes from `GET /recordings/{request_id}`. A new optional query parameter `recorded=true` keeps only the rows that have one.

`geo` is `{"country": "FR", "city": "Paris", "asn": "AS12876"}` when known; any of the three keys may be missing. `next_before` is absent on the last page; pass it back as `before` for the next one.

### GET /whitelist

Caller: app with the admin token gets every unexpired entry. Agent with a server token gets the unexpired entries for that server plus the global ones, nothing else.

Response `200`:

```json
{
  "items": [
    {
      "id": "9a7d3e10-6b2f-4c55-8e0a-1f2b3c4d5e6f",
      "username": "deploy",
      "context": "ssh",
      "server": "web-01",
      "expires_at": "2026-09-14T21:12:07Z",
      "created_at": "2026-09-14T20:12:07Z",
      "created_from_request": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
      "created_by_device": "Pixel 8",
      "created_by_admin": "alice"
    },
    {
      "id": "c1d2e3f4-0000-4aaa-8bbb-ccccdddd1111",
      "username": "ansible",
      "context": "ssh",
      "server": null,
      "expires_at": null,
      "created_at": "2026-09-10T08:00:00Z",
      "created_from_request": null,
      "created_by_device": null
    }
  ]
}
```

`server` null means every server. `expires_at` null means permanent. Expired entries are never returned and are deleted lazily. v1.1: `created_by_admin` names the admin who added the entry, from the app, the dashboard or an `approve_always` verdict; absent in the agent's view of the list.

### POST /whitelist

Caller: app, admin token.

```json
{
  "username": "ansible",
  "context": "sudo",
  "server": "web-01",
  "ttl_seconds": 86400
}
```

`context` defaults to `ssh`. `server` null or omitted means every server. `ttl_seconds` absent or null means permanent. Response `201` with the entry (same shape as the `GET` items) when created, `200` when an existing entry for `(username, context, server)` had its expiry refreshed. `404` if the server name is unknown.

### DELETE /whitelist/{id}

Caller: app, admin token. Response `204`, or `404` if the id is unknown.

Effect timing: logins that reach the backend see the removal immediately. A server whose backend is unreachable keeps its cached copy until the next successful `sync`, at most 5 minutes.

### POST /devices

Caller: app, admin token. Called at first launch and whenever FCM rotates the token. Upsert on `fcm_token`.

```json
{
  "fcm_token": "dEf4...",
  "platform": "android",
  "label": "Pixel 8"
}
```

Response `201` when created, `200` when the token was already known (label updated):

```json
{
  "id": "0c9d1e2f-3a4b-4c5d-8e6f-7a8b9c0d1e2f",
  "platform": "android",
  "label": "Pixel 8",
  "created_at": "2026-09-01T09:00:00Z",
  "last_seen_at": "2026-09-14T20:00:00Z"
}
```

The app stores `id` and sends it as `device_id` in `POST /verdict`. The FCM token is never returned.

### GET /devices

Caller: app, admin token. Response `200` with `items` in the shape above.

### DELETE /devices/{id}

Caller: app, admin token. Response `204`, or `404`. Use it for a lost or replaced phone: it stops receiving pushes at once. History rows keep the label.

### GET /blocked-ips

Caller: app, admin token. Response `200`:

```json
{
  "items": [
    {
      "id": "2b3c4d5e-6f70-4a81-9b92-a3b4c5d6e7f8",
      "ip": "203.0.113.42",
      "reason": "autoblock",
      "denial_count": 3,
      "first_denied_at": "2026-09-14T19:40:12Z",
      "last_denied_at": "2026-09-14T20:12:07Z",
      "hit_count": 12,
      "last_hit_at": "2026-09-14T20:30:01Z",
      "created_at": "2026-09-14T20:12:07Z",
      "expires_at": null
    }
  ]
}
```

`hit_count` is the number of requests denied without a push since the block. `expires_at` null means until unblocked. Expired blocks are not returned.

### DELETE /blocked-ips/{id}

Caller: app, admin token. Response `204`, or `404`. The next request from that IP is pushed normally. Denials keep being counted, so three new denials block it again.

### GET /geo-rules

Caller: app, admin token. Response `200`:

```json
{
  "items": [
    {
      "id": "7e8f9a0b-1c2d-4e3f-8a4b-5c6d7e8f9a0b",
      "country": "KP",
      "note": "no staff there",
      "created_at": "2026-09-14T18:00:00Z"
    }
  ]
}
```

### POST /geo-rules

Caller: app, admin token. `{"country": "KP", "note": "no staff there"}`, country as ISO 3166-1 alpha-2, case-insensitive on input, stored upper case. Response `201` with the rule, `400` for an unknown code, `409` if the country already has a rule.

### DELETE /geo-rules/{id}

Caller: app, admin token. Response `204`, or `404`.

### GET /healthz

No auth. `200 {"status": "ok", "db": "ok"}` when the database answers, `503 {"status": "degraded", "db": "error"}` otherwise. Meant for the container's orchestrator and load balancer; the Scaleway adapter serves it too, it simply is not needed there.

### Push messages (backend to phones)

Also part of the contract: the app parses these. FCM data-only messages (no `notification` block) so the app builds the notification itself, with the action buttons, in every app state. Android priority `high`, FCM TTL 60 s. All values are strings, FCM requires it.

`access_request`, one per phone for every pending request:

```json
{
  "type": "access_request",
  "request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
  "context": "sudo",
  "server": "web-01",
  "username": "deploy",
  "source_ip": "",
  "geo_country": "",
  "geo_city": "",
  "command": "sudo systemctl restart nginx",
  "created_at": "2026-09-14T20:11:40Z",
  "expires_at": "2026-09-14T20:12:10Z"
}
```

Empty string means unknown. The countdown runs from `expires_at`.

`access_notice`: same fields, sent for notify-mode requests. The app shows it without buttons.

`request_decided`, sent to every phone after a verdict so the notification can be dismissed:

```json
{
  "type": "request_decided",
  "request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
  "status": "approved",
  "decided_by_device": "Pixel 8",
  "decided_by_admin": "alice"
}
```

The app must not depend on receiving it: a `409` from `POST /verdict` carries the same information. v1.1 adds `decided_by_admin` (empty string when unknown, as every value is a string); a 1.0.0 app ignores it.

### v1.1 routes

Added on 2026-09-15, additive. Admin routes take an admin token (per-admin or bootstrap) unless said otherwise. Tokens created here are 32 random bytes, base64url, the same format as server tokens, and are returned exactly once: the backend stores the sha256 hash only.

#### GET /admins

Caller: app, dashboard. Response `200`:

```json
{
  "items": [
    {
      "id": "3d5a1c2e-7b8f-4a90-9c1d-2e3f4a5b6c7d",
      "name": "alice",
      "created_at": "2026-09-15T09:00:00Z",
      "disabled_at": null,
      "last_seen_at": "2026-09-15T10:12:07Z"
    }
  ]
}
```

Disabled admins are listed with their `disabled_at` set, so the history names stay explainable. The bootstrap admin is not a row and is not listed; `GET /healthz` is unchanged and does not say whether `ADMIN_TOKEN` is set.

#### POST /admins

Caller: app, dashboard. `{"name": "alice"}`, name 1 to 64 characters, letters, digits, `.`, `_`, `-`. Response `201`:

```json
{
  "id": "3d5a1c2e-7b8f-4a90-9c1d-2e3f4a5b6c7d",
  "name": "alice",
  "token": "dGhpcy1pcy1zaG93bi1vbmNl...",
  "created_at": "2026-09-15T09:00:00Z"
}
```

`token` is shown in this answer only. `409` if the name exists, `400` for a bad name. The same thing from the command line, for the first admin of a fresh deployment: `server admin add --name alice` prints the token once, like `enroll` does for a server.

#### POST /admins/{id}/rotate

Caller: app, dashboard. No body. Response `200` with the same shape as `POST /admins`, `rotated_at` instead of `created_at`. The previous token stops working at once; every phone that used it needs the new one. Rotating a disabled admin re-enables it with the new token (`disabled_at` cleared); the old token never comes back. `404` for an unknown id.

#### DELETE /admins/{id}

Caller: app, dashboard. Disables: sets `disabled_at`, the token stops working at once, the row stays because `requests` and `whitelist` reference it. Response `204`, `404` for an unknown id, `409 {"error": "last enabled admin"}` when it is the only enabled admin and `ADMIN_TOKEN` is not set, so a deployment cannot lock itself out. An admin may disable itself when another one remains.

#### GET /servers

Caller: app, dashboard. Response `200`:

```json
{
  "items": [
    {
      "id": "8c1e2f3a-4b5c-4d6e-8f70-1a2b3c4d5e6f",
      "name": "web-01",
      "os": "linux",
      "created_at": "2026-09-10T08:00:00Z",
      "last_seen_at": "2026-09-15T10:11:40Z",
      "revoked": false
    }
  ]
}
```

`revoked` is true after `DELETE /servers/{id}`. Enrollment stays a script (section 6); this route only lists.

#### POST /servers/{id}/rotate

Caller: app, dashboard. No body. Response `200` `{"id": ..., "name": "web-01", "token": "...", "rotated_at": "..."}`, the token shown once. The old token stops working at once: the agent on that server gets `401` on its next call, which is a final deny (section 3.1, row 5), until `config.json` holds the new token. Rotate, then update the server right away, or use the break-glass file to get on it. `404` for an unknown id, `409 {"error": "server revoked"}` after a revocation.

#### DELETE /servers/{id}

Caller: app, dashboard. Revokes: the stored hash is replaced by a random value, exactly the manual procedure of section 6, so no token matches it any more. The row stays. Response `204`, `404`. A revoked server can be enrolled again under a new name; the old row keeps its history.

#### POST /recordings/{request_id}

Caller: agent, server token. Body: the `script` typescript, `Content-Type: application/octet-stream`, `Content-Encoding: gzip`. Query parameters: `started_at` and `ended_at` (RFC 3339, required), `truncated` (`true` when the agent cut the file at its upload cap, default `false`).

Rules: the request must exist, belong to the server of the token, have context `ssh` and a status of `approved` or `whitelisted`; anything else is `404`. Decompressed size over `RECORDING_MAX_BYTES` (default 20 MiB) is `413`, and the agent then sends the first `record_max_upload_mb` MiB with `truncated=true`. A second upload for the same request is `409`. A deployment without recording storage (`RECORDINGS_DIR` empty, Scaleway today) answers `501 {"error": "recordings not supported on this deployment"}` and the agent stops retrying that file.

Response `201`:

```json
{
  "request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
  "size_bytes": 48213,
  "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "truncated": false,
  "uploaded_at": "2026-09-15T10:30:01Z"
}
```

#### GET /recordings/{request_id}

Caller: app, dashboard, admin token. Streams the typescript back: `200`, `Content-Type: text/plain; charset=utf-8`, `Content-Encoding: gzip` when the client accepts it, `X-Recording-Truncated: true` when the agent cut it, `Content-Disposition: attachment; filename="<request_id>.log"` when the query has `download=true`. `404` when the request has no recording, `501` on a deployment without recording storage. The bytes are the raw typescript with the terminal escape sequences; stripping them for display is the client's job.

#### GET /dashboard

Caller: a browser. Serves the web dashboard's static files (section 3.6), `GET /dashboard` and `GET /dashboard/{file}`. No auth on the files: they contain nothing secret and every call they make carries the admin token the user typed. Not a JSON route, listed here because it is part of what the backend serves.

## 6. Auth model

Principals and credentials:

| Who | Credential | Where it lives | May call |
|---|---|---|---|
| agent on server X | per-server bearer token, 32 random bytes, base64url | `/etc/ssh-sentinel/config.json`, root 0600. Only the sha256 hash is in `servers.token_hash` | `POST /access-request` as X only; `GET /whitelist` for X and global entries |
| phones, dashboard (v1.1: admin accounts) | per-admin bearer token, 32 random bytes, base64url, created by `POST /admins` or `server admin add` and shown once; only the sha256 hash is in `admins.token_hash` | Android encrypted storage on each phone; the browser's session storage for the dashboard | every admin route, as that admin |
| bootstrap admin | `ADMIN_TOKEN`, one shared token, the v1 model kept as is | the backend's environment (Secret Manager on Scaleway, `.env` with compose) | every admin route, as the admin named `bootstrap`. Optional from v1.1: unset it once named admins exist |
| backend to database | connection string (Scaleway IAM key, or the compose password) | backend environment, `DATABASE_URL` | |
| backend to FCM | Firebase service account key, exchanged for a short-lived OAuth2 token | backend environment, `FCM_SERVICE_ACCOUNT_JSON` | FCM HTTP v1 send |

Rules:

- Tokens are compared in constant time against the stored hash. A database dump does not yield usable server tokens.
- The server identity always comes from the token. The payload's `hostname` is informational.
- `device_id` is attribution of the phone, not authentication: the admin token is what authorizes a verdict. v1.1: the token also carries the admin identity; the history shows both, the admin name (`decided_by_admin`) and the phone label (`decided_by_device`). With the bootstrap token alone, as in 1.0.0, the name is `bootstrap` and the device label is the only thing that tells phones apart.
- Admin token lookup (v1.1): sha256 of the presented token, looked up in `admins` (unique index, disabled rows excluded), then compared in constant time with `ADMIN_TOKEN` when set. No match is `401`. `last_seen_at` is updated on every call.
- Server enrollment: a script generates the token, inserts the hash and name into `servers`, prints the plain token once. The installer writes it into the agent config. No enrollment endpoint; v1.1 adds `GET /servers` to list, `POST /servers/{id}/rotate` and `DELETE /servers/{id}` to rotate and revoke.
- Rotation (v1.1): `POST /admins/{id}/rotate` and `POST /servers/{id}/rotate` replace the stored hash and return the new token once; the old token is dead the moment the call returns. A rotated server denies every login until its config holds the new token, so rotate and update the server in the same sitting, with the break-glass file as the safety net. The bootstrap token rotates the old way: change the secret and redeploy. The manual SQL of 1.0.0 (replace `token_hash` by hand) still works.
- Disabling an admin (`DELETE /admins/{id}`) keeps the row: history and whitelist entries reference it by id and keep showing the name. The last enabled admin cannot be disabled while `ADMIN_TOKEN` is unset.
- Transport is HTTPS only. On Scaleway the function URL is TLS-terminated by the platform; with compose, Caddy or the operator's proxy terminates TLS in front of the container, which speaks plain HTTP on its internal network only. The agent trusts the OS certificate store; minimal images need the `ca-certificates` package.
- A stolen server token allows creating requests for that server (push noise) and reading that server's whitelist. It cannot approve anything. The admin notices the noise and revokes the server by replacing its `token_hash` with a random value: the agent then gets 401, which is a final deny (section 3.1, row 5), and the cache does not override it. The row is not deleted, `requests` references it.
- A stolen admin token allows approving anything, unblocking IPs, removing rules and, from v1.1, creating admins and rotating servers. Hence encrypted storage on the phones, session storage (cleared when the tab closes) in the dashboard, and revocation by rotating or disabling that admin; the bootstrap token is revoked by changing the secret and redeploying. Per-admin tokens make the blast radius one admin instead of everyone, and the history says which one.
- Recordings (v1.1) are read with an admin token and written with the server token of the server the request belongs to; a server cannot upload for another server's request. The typescript can contain anything the user typed or saw, passwords included when a program echoed them: the recordings directory is root-only on the server and the storage volume is the backend's; treat it like the database.
- The break-glass file is root-only, never synced, never leaves the server. It is the escape hatch for a backend outage. Keep it to one or two accounts and review it.
- Secrets never go in git: `.gitignore` covers `.env*`, key material, `*.tfvars`, `google-services.json` and service account files.

## 7. Failure modes

"Cache" means the synced whitelist in `/var/lib/ssh-sentinel/whitelist.json`, unexpired entries only.

| # | Situation | Break-glass user | User with an active whitelist entry (backend or cache) | Any other user | Where it shows |
|---|---|---|---|---|---|
| 1 | All up, an admin taps Approve | allow, no network | allow, no push | allow | history: `approved` / `whitelisted` |
| 2 | All up, an admin taps Deny | allow | allow, no push | deny, counts toward auto-block | history: `denied` |
| 3 | All up, an admin taps Always allow, with or without TTL | allow | allow | allow, entry created or refreshed | history: `approved`, whitelist |
| 4 | Nobody answers within the window | allow | allow, no push | deny | history: `timeout` |
| 5 | Every phone offline | allow | allow | deny after the window | history: `timeout`; apps show it later |
| 6 | Push arrives after expiry, someone taps | allow | allow | already denied; app gets `409` `timeout` | history: `timeout` |
| 7 | Two admins answer | allow | allow | first verdict wins; second gets `409` with the winner's label; other phones get `request_decided` | history: `decided_by_device` |
| 8 | Source IP is in `blocked_ips` | allow | deny, no push (rules run before the whitelist) | deny, no push | history: `blocked_ip`; blocked IPs screen |
| 9 | Country matches a geo rule | allow | deny, no push | deny, no push | history: `blocked_geo` |
| 10 | Geo lookup fails while geo rules exist | allow | allow, no push | pushed with country unknown, an admin decides | history: `geo` null |
| 11 | Whitelist entry expired | allow | treated as any other user; the cache drops it at the same instant by the server's clock | | history: normal flow |
| 12 | Backend or database down (DNS, connect, TLS, 5xx, no answer in 30 s) | allow | allow from cache | deny | server syslog only |
| 13 | Backend rejects the server token (401/403) | allow | deny | deny | server syslog, backend logs |
| 14 | FCM API error or rejected token | allow | allow, no push needed | deny after the window | history: `timeout`, backend logs the FCM error |
| 15 | An admin approves after the agent gave up (network slower than the 5 s margin) | allow | allow | denied on the server although history says approved; a retry gets a new push | history: `approved`, syslog: timeout |
| 16 | Agent binary missing, not executable, or crashing | deny | deny | deny | PAM logs the `pam_exec` failure |
| 17 | Config file missing or unreadable | allow (fixed path, read first) | allow from cache (fixed path) | deny | server syslog |
| 18 | Notify mode (Windows, or Linux during rollout) | allow | allow | allow, informational push | history: `notified` |
| 19 | Same user twice at once | allow | allow | two requests, two pushes, each decided on its own | history: two rows |
| 20 | An admin's own IP got auto-blocked (a colleague denied by mistake) | allow | deny | deny; unblock from the blocked IPs screen on any phone | blocked IPs screen |
| 21 | sudo with the backend down | allow | allow from cache, sudo entries only | deny | server syslog |
| 22 | Several container replicas or function instances | no effect: the verdict wait goes through the database | | | |
| 23 | Login from the console, or a service other than sshd and sudo | unaffected | unaffected | unaffected, only those two PAM files are changed | nothing |
| 24 | Clock skew on the server | no effect on verdicts; cache expiry uses the server clock, keep NTP on | | | |

Row 16 is the one that bites: the break-glass file only helps while the binary runs. Keep console access (cloud provider console, IPMI, or a second sshd on another port during rollout), start new servers in notify mode, and test the installer on a VM first.

Per-admin tokens (v1.1). The verdict rows above do not change; what changes is who is named:

| # | Situation | Effect | Where it shows |
|---|---|---|---|
| 25 | An admin's token is stolen | rotate or disable that admin from any phone or the dashboard; the other admins keep working. With the bootstrap token alone, as in 1.0.0, change the secret and redeploy | admins screen, history names the admin |
| 26 | Every named admin disabled or lost, `ADMIN_TOKEN` unset | nobody can call an admin route. Recovery: `server admin add` on the database, or set `ADMIN_TOKEN` and redeploy. Prevented for the last one by the `409` of `DELETE /admins/{id}` | backend logs `401` |
| 27 | A server token is rotated before the config is updated | every login on that server is denied (row 13) until the new token is in `config.json`; break-glass users still get in | server syslog `reason=rejected` |
| 28 | A 1.0.0 app or agent talks to a v1.1 backend | works unchanged: the new fields are ignored, the new routes are not called | nothing |

Session recording (v1.1). A recording problem never changes the verdict; the approval is the boundary, the recording is the audit trail:

| # | Situation | Session | Recording | Where it shows |
|---|---|---|---|---|
| 29 | Approved SSH login, `record_sessions` on, everything up | opens under `script` | uploaded at session end, badge in history | history `recording`, dashboard playback |
| 30 | `script` missing, or the recordings directory not writable or full | opens unrecorded | none | server syslog `recording=skipped` |
| 31 | Backend unreachable when the session ends | unaffected, already open | stays local, retried by `sync` every 5 minutes, deleted by rotation if the outage outlasts `record_max_total_mb` | server syslog |
| 32 | Recording larger than the caps | unaffected | uploaded up to `record_max_upload_mb`, marked `truncated` | badge says truncated |
| 33 | Backend without recording storage (Scaleway today) | unaffected | `501`, kept locally, not retried; visible on the server only | server syslog once per file |
| 34 | The user tampers with the file during the session | unaffected | append-only attribute: appends go through, truncation and edits fail. A file system without the attribute (not ext4 or xfs) leaves the file editable by the user | server syslog on the `chattr` failure |
| 35 | Break-glass, cache-approved during an outage, notify mode, whitelisted from the cache | as before | none: no backend request id, or nothing enforced | nothing |
| 36 | scp, sftp, or `ssh host command` without a tty | passes through unchanged | none (`script` needs a tty); commands with a tty (`ssh -t`) are recorded | nothing |
| 37 | sshd reloaded with a broken `ForceCommand` line | every login refused | | the installer validates with `sshd -t` before reloading and restores the backup on failure |
| 38 | The agent binary crashes inside the wrapper | the user's shell never starts, session closes | none | server syslog. Rare, and the reason the wrapper stays tiny |

## 8. Design notes and decisions

Accepted on 2026-09-14, review of the scaffolding:

1. PAM phase is `account`, line added before the distro's first `account` include in `/etc/pam.d/sshd` and `/etc/pam.d/sudo` (`@include common-account` on Debian and Ubuntu, `account include password-auth` or `system-auth` on the RHEL family). Before, not after: on the RHEL family the included file contains `account sufficient pam_localuser.so`, which ends the stack for local users, so a line placed after the include would be silently skipped. `install.sh` does it this way. sshd skips the PAM `auth` stack for public-key logins but runs `account` for every login method once the credentials are valid, so the admins are only asked about logins that would otherwise succeed. `required` means a non-zero exit fails the login.
2. `POST /devices` is in the contract.
3. Server enrollment is a script, no endpoint.
4. macOS is best effort, later. Apple does not ship `pam_exec`; FreeBSD has one for OpenPAM, which macOS uses, so a port is the likely route. Nothing blocks on it.
5. One version: sudo approval, temporary access, multi-admin, auto-block, geo rules, fail2ban filter are in scope.
6. Portability: core `net/http`, Docker container plus Scaleway adapter, PostgreSQL 14+ only, FCM everywhere.

Choices made while applying the scope changes. Flagged so they can be reversed before the components exist:

7. Whitelist entries are per context. "Always allow" on a sudo request whitelists sudo for that user, not SSH, and the other way round. Break-glass covers both.
8. Rules run before the whitelist. A whitelisted user from a blocked IP or country is denied; unblocking from the app is the fix, break-glass is the safety net.
9. Geo lookup failure with geo rules configured: the request is pushed with the country unknown, an admin decides. The alternative (deny) was not chosen because the backend is reachable and a human is available.
10. `mode` on `/access-request`. Windows always sends `notify`. A Linux server can be installed in notify mode, watched in history, then switched to enforce, which makes the rollout safe.
11. Scaleway runs one function, `api`, serving every route. Clients get one base URL on every provider. Splitting into per-route functions later would need a gateway in front to keep that promise.
12. `GET` and `DELETE /devices` next to `POST`, so a lost phone can be removed. Without them the only fix would be a manual `DELETE` in the database.
13. `request_decided` push after a verdict, so the other phones dismiss the notification. Apps must work without it.
14. Auto-block counts admin denials only, not timeouts, so a phone left in a drawer never blocks anyone. Defaults: 3 denials in 1 hour, blocked until unblocked. All three are configuration.
15. sudo: `pam_exec` provides no remote host and no command. `source_ip` is null (the app shows "local"), `command` is read from the parent sudo process in `/proc` as a best effort.
16. Expiry is enforced by whoever decides, with its own clock: the backend for requests that reach it, the agent cache for outages. Keep NTP on the servers.
17. Verdict wait through the database on every deployment. It works across function instances and container replicas alike, and keeps one code path.
18. fail2ban is the per-server layer, auto-block the fleet-wide one. Both count denials; they do not talk to each other.

Decided on 2026-09-15, after the 1.0.0 release. Three features move from out of scope into scope as contract v1.1, additive only; the choices below are flagged so they can be reversed before the code exists:

19. Contract v1.1, not v2. Every addition is a new route, a new nullable column or a new optional field; a 1.0.0 agent or app keeps working. `POST /verdict` keeps `device_id` optional, so the dashboard can decide without a device.
20. Per-admin accounts through a new `admins` table; the bootstrap `ADMIN_TOKEN` stays and acts as the admin named `bootstrap`, so no existing setup breaks. Admin and server tokens share one format and one rule: shown once, hash stored. `last_seen_at` was added to the requested columns for parity with `servers` and `devices`.
21. Disable, never delete: `DELETE /admins/{id}` sets `disabled_at`, the same pattern as `devices.deleted_at`, because history and whitelist rows reference the admin. Rotating a disabled admin re-enables it; that is the only re-enable path. The last enabled admin cannot be disabled while `ADMIN_TOKEN` is unset.
22. Server routes beyond the request: `GET /servers` is needed for the dashboard and the app to pick a server to rotate, and `DELETE /servers/{id}` is the revocation procedure of decision 9 as a route. Enrollment stays a script. Both are easy to drop if unwanted.
23. `decided_by_admin` is added to the history item, both `POST /verdict` bodies, the `request_decided` push and, as `created_by_admin`, to whitelist entries. `decided_by_device` stays what it was.
24. Session recording is SSH only, enforce mode only, and only when the backend returned a request id (reasons `admin` and `whitelist`). sudo is not recorded: `sudo` runs the command itself, and its own I/O logging exists for that.
25. Recording mechanism: sshd `ForceCommand` running `ssh-sentinel session` as the user, with a ticket left by `check` in the PAM `account` phase, and `script -q -f -a`. PAM cannot wrap a shell and a `session`-phase hook cannot reach the tty, so the wrapper is the portable way. sftp and scp pass through unrecorded. The wrapper carries no token; uploads are done by root from the `sync` timer.
26. Recording failures never refuse an approved session. The verdict is the security control, the recording is the audit trail; a missing `script` or a full disk must not lock the admins out.
27. Tamper resistance by the append-only file attribute set by root at ticket time, not by a privileged recorder. Honest limit: an audit aid, not evidence. A root-owned recorder is the follow-up if evidence-grade recording is ever needed.
28. Recording storage is a local directory on the backend (`RECORDINGS_DIR`, a compose volume), behind a one-method storage interface. Scaleway functions have no persistent disk: on Scaleway the recording routes answer `501` and the agent keeps the files locally. Object Storage behind the same interface is the documented limitation to lift; it is not part of v1.1.
29. Caps: `RECORDING_MAX_BYTES` on the backend (default 20 MiB decompressed), `record_max_upload_mb` on the agent (default 20, sends the head and flags `truncated`), `record_max_total_mb` for the local directory (default 500, oldest first, uploaded first). Recordings stay on the server after upload as the second copy.
30. Web dashboard in `web/`, embedded in the backend binary with `go:embed` and served at `/dashboard`: one deployment step less, same origin as the API so no CORS, exists on Scaleway too. No framework and no build chain, vendored single-file helpers only. It is a client of the contract, never a private API.
31. Dashboard login is the admin token in session storage, sent as the bearer. No cookies, no server-side sessions, no CSRF surface; the API stays what it is. Live pending requests are polled every 2 s through `GET /history?status=pending`; server-sent events are the follow-up if polling ever matters.
32. Still out of scope: iOS, Windows blocking mode.

Still to check during implementation:

- Recording on distros without `/etc/ssh/sshd_config.d/` (Debian 11, RHEL 8): the installer appends an `Include` line to `sshd_config`; confirm on a VM that `ForceCommand` in the included file wins over a `Match` block the operator may have.
- `chattr +a` needs CAP_LINUX_IMMUTABLE, which root has, and a file system that supports the attribute: ext4 and xfs do, tmpfs and some overlay setups do not. The installer checks the recordings directory once and warns.
- `script` flags differ between util-linux (Linux) and the BSD one; the wrapper targets util-linux 2.31 or newer (`-q -f -a`, `-c`), which every target distro ships.
- `ForceCommand` and `internal-sftp`: the wrapper must exec the sftp server binary when `SSH_ORIGINAL_COMMAND` is `internal-sftp`; the path differs per distro (`/usr/lib/openssh/sftp-server`, `/usr/libexec/openssh/sftp-server`).

- SELinux on the RHEL family: `pam_exec` runs the agent inside the sshd and sudo contexts; outbound HTTPS may be denied under enforcing mode. Check `ausearch -m avc` on the first Rocky/Alma test, ship a small policy module in `install.sh` if needed.
- sshd limits: `LoginGraceTime` (120 s default) covers the wait; `MaxStartups` counts connections waiting in PAM.
- Windows watcher: OpenSSH Server on Windows writes to the `OpenSSH/Operational` channel and the Security log (event 4624). Exact filter picked on a Windows VM.
- Geo provider: bundled GeoLite2 database vs an HTTP lookup, 1 s timeout, results cached per IP for a day. Must never delay the push.
- History retention: keep everything for now, retention job later.
