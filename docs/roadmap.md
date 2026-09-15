# Roadmap

One version. The earlier v1/v2 split was merged on 2026-09-14. The API contract (`docs/architecture.md`, section 5) is frozen and the four components are built in parallel against it.

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

### By component, in parallel

Step 1, agent (`agent/`): `check` for ssh and sudo, `sync` with expiry, `watch`, notify mode, `install.sh` for apt and dnf/yum families, fail2ban filter, static builds for linux/amd64, linux/arm64, windows/amd64.

Step 2, backend (`backend/`): core handlers for every route in contract v1, rules engine, verdict wait through the database, FCM push, container entrypoint and `Dockerfile`, Scaleway adapter, numbered migrations that run on PostgreSQL 14.

Step 3, infra (`infra/`): `scaleway/` Terraform with dev and prod, `docker-compose/` with backend, PostgreSQL and optional TLS proxy, `.env.example`.

Step 4, mobile (`mobile/`): six screens, notification action buttons handled in the background, device registration, TTL picker, `409` handling.

Docs: README per component, architecture with the frozen contract, this roadmap, an install guide for servers with the lockout recovery procedure, an enrollment guide (server tokens, phone registration).

## Out of scope

- iOS app
- Windows blocking mode (Windows stays notify-only)
- session recording
- web dashboard
- SIEM or audit export
- rate limiting beyond auto-block
- token rotation endpoints (server tokens, admin token)
- per-admin tokens or accounts (one admin token shared by the phones; the device label gives attribution, not authentication)

## Best effort, later

- macOS agent. Apple does not ship `pam_exec`; FreeBSD has one for OpenPAM, which macOS uses, so a port is the likely route. Nothing blocks on it, no date.
- Other serverless adapters (AWS Lambda, Google Cloud Run functions, ...) with matching `infra/` folders.
- History retention job.
