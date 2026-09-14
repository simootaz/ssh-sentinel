# ssh-sentinel - SSH and sudo approval from your phone

Every SSH login and every sudo on a protected server sends a push notification to the admins' phones. An admin taps Approve, Deny or Always allow. No answer within 30 seconds means no access.

## How it works

1. sshd or sudo hands the action to PAM, PAM runs `ssh-sentinel check`.
2. The agent checks a local break-glass list first, then asks the backend.
3. The backend applies the rules (blocked IPs, country blocklist, whitelist), stores the request, pushes it to every registered phone, and waits for the first verdict.
4. The app answers, the backend passes the verdict back, the agent exits 0 (allow) or 1 (deny).

Fail-closed by design: backend unreachable and unknown user means deny. Flow, database schema, the frozen API contract, auth model and failure modes are in [docs/architecture.md](docs/architecture.md).

The backend is plain Go `net/http`: run it as one Docker container next to any PostgreSQL 14+, or on Scaleway Serverless Functions. Push notifications use FCM either way.

## Repository layout

| Folder | What | Docs |
|---|---|---|
| `agent/` | Go binary installed on servers: PAM hook for ssh and sudo, Windows watcher, installer, fail2ban filter | [agent/README.md](agent/README.md) |
| `backend/` | Go API: core handlers, container entrypoint, Scaleway adapter, SQL migrations | [backend/README.md](backend/README.md) |
| `infra/scaleway/` | Terraform for the reference deployment on Scaleway | [infra/README.md](infra/README.md) |
| `infra/docker-compose/` | Universal deployment: backend container + PostgreSQL | [infra/README.md](infra/README.md) |
| `mobile/` | Flutter app, Android first | [mobile/README.md](mobile/README.md) |
| `docs/` | architecture, frozen contract, roadmap | [docs/](docs/) |

## Status

Design accepted, API contract v1 frozen. The four components are built in parallel against it. See [docs/roadmap.md](docs/roadmap.md).

## Contributing

Git flow: feature branches off `develop`, conventional commit messages. Never test PAM changes on a production server; use a throwaway VM and keep a root shell open while you do.

## License

See [LICENSE](LICENSE).
