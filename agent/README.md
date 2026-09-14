# agent

Go binary installed on every protected server. One static executable, no runtime dependencies.

Targets: Linux (Ubuntu, Debian, AlmaLinux, Rocky, RHEL) with enforcement, Windows notify-only. macOS is best effort, later; nothing here depends on it.

## Role

- `ssh-sentinel check` is called by PAM through `pam_exec`, in the `account` phase, for every SSH login (`/etc/pam.d/sshd`) and, with `--context=sudo`, for every sudo invocation (`/etc/pam.d/sudo`). It decides whether the action goes through:
  1. Look the user up in the local break-glass whitelist. Match = allow, no network call at all. Applies to both contexts.
  2. Otherwise `POST /access-request` to the backend and wait up to 30 seconds for the verdict.
  3. Exit `0` to allow, `1` to deny. Timeout, unreachable backend or any error = deny, unless the user has an unexpired entry for that context in the whitelist cache synced from the backend (decision table in `docs/architecture.md`, section 3.1).
- `ssh-sentinel sync` refreshes the local copy of the backend whitelist for this server, expiry included. Runs from a systemd timer every 5 minutes.
- `ssh-sentinel watch` (Windows) follows the event log and reports each SSH login to the backend in notify mode. It never blocks a login.
- Mode `enforce` (default) or `notify` in the config. In notify mode `check` always exits 0; the backend records the request and sends an informational push. Use it to roll a new server out and watch the history before enforcing.
- `scripts/install.sh` installs the binary, detects apt vs dnf/yum, writes the two PAM lines, the config file and the sync timer. When fail2ban is installed it also drops the filter from `scripts/fail2ban/`.

## Layout

| Path | Content |
|---|---|
| `cmd/ssh-sentinel/` | `main` package, subcommand dispatch (`check`, `sync`, `watch`, `version`) |
| `internal/check/` | PAM flow: break-glass, backend call, cache fallback, exit code, sudo command lookup |
| `internal/watch/` | Windows event-log watcher |
| `internal/whitelist/` | break-glass file and synced whitelist cache, with expiry |
| `internal/client/` | HTTP client for the backend (bearer token, timeouts) |
| `internal/config/` | config file loading |
| `scripts/` | `install.sh`, PAM templates per distro family in `scripts/pam/`, fail2ban filter and jail example in `scripts/fail2ban/` |

Files on a server (Linux):

| Path | Purpose | Owner / mode |
|---|---|---|
| `/usr/local/bin/ssh-sentinel` | the binary | `root 0755` |
| `/etc/ssh-sentinel/config.json` | backend base URL, server token, mode, timeouts | `root 0600` |
| `/etc/ssh-sentinel/breakglass` | break-glass whitelist, one username per line, hand-edited only, never synced | `root 0600` |
| `/var/lib/ssh-sentinel/whitelist.json` | cache of the backend whitelist for this server, with expiry, written by `sync` | `root 0600` |
| `/etc/fail2ban/filter.d/ssh-sentinel.conf` | fail2ban filter, copied by the installer when fail2ban is present | `root 0644` |

JSON for the config because Go parses it with the standard library, so no extra dependency.

PAM lines written by the installer, after the distro's existing `account` lines:

```
# /etc/pam.d/sshd
account required pam_exec.so quiet /usr/local/bin/ssh-sentinel check
# /etc/pam.d/sudo
account required pam_exec.so quiet /usr/local/bin/ssh-sentinel check --context=sudo
```

## Log format

Every decision is one syslog line (`auth` facility) made of `key=value` pairs. The format is stable: fail2ban and operators parse it.

```
ssh-sentinel[2131]: decision=deny context=ssh user=deploy rhost=203.0.113.42 host=web-01 reason=admin request=5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11
ssh-sentinel[2140]: decision=allow context=sudo user=deploy rhost=- host=web-01 reason=whitelist request=9a7d3e10-6b2f-4c55-8e0a-1f2b3c4d5e6f
```

| Key | Values |
|---|---|
| `decision` | `allow`, `deny` |
| `context` | `ssh`, `sudo` |
| `user` | the PAM user |
| `rhost` | source IP, `-` when unknown (sudo) |
| `host` | this server's hostname |
| `reason` | `breakglass`, `admin`, `whitelist`, `cache`, `notify`, `timeout`, `unreachable`, `rejected`, `blocked_ip`, `blocked_geo`, `error` |
| `request` | request id from the backend, `-` when the backend was never reached |

The fail2ban filter matches `decision=deny` lines that carry an `rhost`, so a jail can ban an IP locally after a few denials on that server. This is a local layer; the backend's auto-block is the fleet-wide one. Windows writes the same line to the Application event log.

## Build

Go 1.22 or newer. From `agent\` on the Windows dev machine (PowerShell):

```powershell
go mod tidy
go build -o bin\ssh-sentinel.exe .\cmd\ssh-sentinel
```

Static Linux binaries, cross-compiled from Windows. `CGO_ENABLED=0` makes the file fully static, `-s -w` strips debug symbols, `-trimpath` removes local paths from the binary.

```powershell
$env:CGO_ENABLED = "0"
$env:GOOS = "linux";   $env:GOARCH = "amd64"; go build -trimpath -ldflags "-s -w" -o bin\ssh-sentinel-linux-amd64 .\cmd\ssh-sentinel
$env:GOOS = "linux";   $env:GOARCH = "arm64"; go build -trimpath -ldflags "-s -w" -o bin\ssh-sentinel-linux-arm64 .\cmd\ssh-sentinel
$env:GOOS = "windows"; $env:GOARCH = "amd64"; go build -trimpath -ldflags "-s -w" -o bin\ssh-sentinel-windows-amd64.exe .\cmd\ssh-sentinel
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```

A darwin/arm64 build is one more line and worth keeping in the release script, but nothing is tested on macOS.

## Test

```powershell
go vet ./...
go test ./...
```

Unit tests cover the decision logic against a fake backend (Go's `httptest` package) and a fake clock for cache expiry, so they run on Windows without PAM.

End-to-end tests run on a throwaway VM only (Multipass, VirtualBox or a cloud instance). Never on a production server.

1. Copy the Linux binary and `scripts/install.sh` to the VM, run the installer in notify mode first.
2. Keep a root shell open on the VM before touching PAM. A broken PAM config locks you out of SSH; the open shell, or the console, is the way back.
3. SSH in as a normal user: expect an informational push and an immediate login. Switch the config to enforce mode.
4. SSH in again: expect a push with buttons and a wait of up to 30 s. Run `sudo -i` in that session: expect a second push marked sudo.
5. Tap Always allow with a 1 h TTL, run `sudo -i` again: no push. Run `ssh-sentinel sync`, check the entry and its expiry in the cache file.
6. Point the config at an unreachable backend URL. SSH in as a break-glass user: immediate allow. As the user with the cached entry: allow. As an unknown user: deny.
7. With fail2ban installed, deny three logins from a test client and check `fail2ban-client status ssh-sentinel`.

## Notes

- RHEL family with SELinux enforcing: check `ausearch -m avc -ts recent` after the first test. The PAM contexts of sshd and sudo may need a small policy addition to allow the outbound HTTPS call.
- sshd `LoginGraceTime` (default 120 s) covers the 30 s wait. `MaxStartups` counts connections waiting in PAM.
- For sudo, `pam_exec` gives no remote host and no command. The agent reads the command line of its parent sudo process from `/proc` as a best effort and sends it when found; `source_ip` is sent as null.
- Cache expiry uses the server clock. Keep NTP running.
- macOS: Apple does not ship `pam_exec`. FreeBSD has one for OpenPAM, which macOS uses, so a port is the likely route when the time comes. Not scheduled.
