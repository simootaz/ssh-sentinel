# Roadmap

One version. The earlier v1/v2 split was merged on 2026-09-14. The API contract (`docs/architecture.md`, section 5) is frozen and the four components were built in parallel against it; 1.0.0 was released on 2026-09-15.

On 2026-09-15 three features moved from out of scope into scope, as contract v1.1: per-admin accounts with token rotation, session recording, and a web dashboard. v1.1 is additive only: a 1.0.0 agent or app keeps working against a v1.1 backend. See "v1.1" below.

## Scope

Goal: a few admins, a fleet of Linux servers, every SSH login and every sudo needs a tap on a phone unless the user is on the whitelist. Fail-closed. Keeps working during a backend outage for break-glass and already-whitelisted users. Runs on any provider.

Done when:

- an SSH login on a fresh Ubuntu VM and a fresh Rocky VM triggers a push within a few seconds, and `sudo` in that session triggers a second one marked sudo,
- Deny, Approve and Always allow (with a TTL) work from the notification on two phones, and the slower phone sees who decided,
- with the backend unreachable, a break-glass user gets in immediately, a user with an unexpired cached entry gets in, anyone else is refused,
- three denials from one IP block it fleet-wide without a push, and unblocking from the app works,
- a country rule blocks a login without a push and shows in history,
- a Windows server shows up in history as notify-only,
- the same backend code runs from `infra/docker-compose/` on a plain VM and from `infra/scaleway/`, with no secret in git.

### Features

| Feature | What it does |
|---|---|
| SSH approval | PAM `account` hook, push, 30 s verdict, exit code |
| sudo approval | second PAM line in `/etc/pam.d/sudo`, `ssh-sentinel check --context=sudo`, the push shows the command when the agent can read it |
| break-glass | local root-only file, checked before any network call, works offline |
| whitelist with expiry | `approve_always` with an optional TTL (1 h, 24 h, custom, permanent); `whitelist.expires_at`; backend and agent cache both enforce it |
| multi-admin | several phones registered through `POST /devices`, push to all, first verdict wins, `409` for the others, `request_decided` push to dismiss |
| auto-block | N admin denials from one IP inside a window (both configurable) add the IP to `blocked_ips`; further requests are denied without a push, status `blocked_ip`; list and unblock from the app |
| geo rules | country blocklist in `geo_rules`, evaluated at request time, denied without a push, status `blocked_geo`; list, add, remove from the app |
| fail2ban | stable syslog line format, filter and jail example shipped with the agent |
| notify mode | Windows always, Linux optionally during rollout: informational push, never blocks |
| portability | core `net/http` handlers; one Docker container next to any PostgreSQL 14+, or Scaleway Serverless Functions through an adapter; FCM everywhere |
| per-admin accounts (v1.1) | `admins` table, one token each, shown once; `ADMIN_TOKEN` stays as the bootstrap admin; history and whitelist name the admin (`decided_by_admin`, `created_by_admin`); `GET/POST /admins`, `POST /admins/{id}/rotate`, `DELETE /admins/{id}` (disable); managed from the app's Settings and the dashboard |
| token rotation (v1.1) | `POST /admins/{id}/rotate` and `POST /servers/{id}/rotate` return the new token once, the old one dies at once; `GET /servers` and `DELETE /servers/{id}` (revoke) alongside; enrollment stays a script |
| session recording (v1.1) | agent: on an approved SSH login in enforce mode, `script(1)` typescript into `/var/lib/ssh-sentinel/recordings/<request-id>.log` through an sshd `ForceCommand` wrapper, `record_sessions` flag, rotation by `record_max_total_mb`, gzip upload to `POST /recordings/{request_id}`; backend: `recordings` table plus the blob on disk (`RECORDINGS_DIR`), `GET /recordings/{request_id}` streams it back; history rows carry a `recording` field, the app shows a badge, the dashboard plays it as plain text. Scaleway has no persistent disk: recordings answer `501` there until an Object Storage backend exists (documented limitation) |
| web dashboard (v1.1) | `web/`, single-page app without a build chain, embedded in the backend binary with `go:embed`, served at `/dashboard`; admin-token login; live pending requests with the three buttons, history with filters, whitelist, blocked IPs, geo rules, admins, servers, recording playback |

### By component, in parallel

Step 1, agent (`agent/`): `check` for ssh and sudo, `sync` with expiry, `watch`, notify mode, `install.sh` for apt and dnf/yum families, fail2ban filter, static builds for linux/amd64, linux/arm64, windows/amd64.

Step 2, backend (`backend/`): core handlers for every route in contract v1, rules engine, verdict wait through the database, FCM push, container entrypoint and `Dockerfile`, Scaleway adapter, numbered migrations that run on PostgreSQL 14.

Step 3, infra (`infra/`): `scaleway/` Terraform with dev and prod, `docker-compose/` with backend, PostgreSQL and optional TLS proxy, `.env.example`.

Step 4, mobile (`mobile/`): six screens, notification action buttons handled in the background, device registration, TTL picker, `409` handling.

Docs: README per component, architecture with the frozen contract, this roadmap, an install guide for servers with the lockout recovery procedure, an enrollment guide (server tokens, phone registration).

### v1.1, after 1.0.0

Contract v1.1 is written (`docs/architecture.md`, section 5, "v1.1 routes", and section 8, decisions 19 to 32). The work, in the order the dependencies suggest; the backend goes first because the other three build on its routes:

Step 5, backend: migrations `003_admins.sql` and `004_recordings.sql`; admin identity in `internal/auth` with the bootstrap fallback; `server admin add`; the admins, servers and recordings routes; `decided_by_admin` and `recording` on the existing responses; `RECORDINGS_DIR` and `RECORDING_MAX_BYTES`; `501` without storage; `/dashboard` served from `go:embed`; compose volume `recordings`; unit tests for every new route including the `409` on the last admin and the ownership check on uploads.

Step 6, web (`web/`): the single-page app, vendored helpers only, CSP-clean (no inline scripts); every screen listed in the features table; token in session storage; polling of pending requests; plain-text recording viewer with escape sequences stripped; a Go test that serves the embedded files and checks the CSP header.

Step 7, agent: `session` wrapper and `upload`; the ticket and the append-only file written by `check`; `record_*` config keys; `sync` retrying uploads and rotating the directory; `install.sh` writing the sshd drop-in, validating with `sshd -t`, and the systemd path unit; syslog lines `recording=...`; unit tests with a fake `script` and a fake backend, VM checks in `agent/TESTING.md`.

Step 8, mobile: Settings gets the Admins section (list, add with the token shown once, rotate, disable); `decided_by_admin` in the incoming request outcome and in history; recording badge and plain-text view; widget tests on the fake API.

Done when, on top of the 1.0.0 list:

- two named admins on two phones, the history says which admin and which phone decided, and rotating one admin locks out only that phone until it gets the new token,
- disabling an admin stops its phone at once and the `409` refuses to disable the last one,
- rotating a server from the dashboard denies its next login until `config.json` is updated, and the break-glass user still gets in meanwhile,
- an approved SSH login on the Ubuntu VM with `record_sessions` on produces a recording that shows up as a badge in the app and plays in the dashboard, and an scp to the same VM leaves no recording,
- the same login on the Scaleway deployment shows `501` in the server syslog, keeps the file locally, and nothing else changes,
- the dashboard approves a pending request without a phone, and the history row shows the admin name with no device.

## Out of scope

- iOS app
- Windows blocking mode (Windows stays notify-only)
- SIEM or audit export
- rate limiting beyond auto-block
- recordings on Scaleway (Object Storage behind the storage interface): a documented limitation of v1.1, not a v1.1 deliverable
- evidence-grade session recording (a privileged recorder): v1.1 recordings are an audit aid with an append-only file, not evidence
- sudo session recording: `sudo` has its own I/O logging

## Best effort, later

- macOS agent. Apple does not ship `pam_exec`; FreeBSD has one for OpenPAM, which macOS uses, so a port is the likely route. Nothing blocks on it, no date.
- Other serverless adapters (AWS Lambda, Google Cloud Run functions, ...) with matching `infra/` folders.
- History retention job.
