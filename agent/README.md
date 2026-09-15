# agent

Go binary installed on every protected server. One static executable, no runtime dependencies, standard library only.

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
| `internal/watch/` | Windows event-log watcher: wevtutil polling, sshd message parser, notify-mode reporting |
| `internal/whitelist/` | break-glass file and synced whitelist cache, with expiry |
| `internal/client/` | HTTP client for the backend (bearer token, timeouts, rejected vs unreachable errors) |
| `internal/config/` | config file loading and validation |
| `internal/auditlog/` | the decision line; syslog on Linux, Application event log on Windows |
| `scripts/install.sh` | Linux installer and uninstaller |
| `scripts/install_test.sh` | checks the PAM insertion against Debian and RHEL layouts in a temp dir |
| `scripts/pam/` | `sshd.ssh-sentinel`, `sudo.ssh-sentinel`: reference copies of the two PAM lines |
| `scripts/fail2ban/` | `ssh-sentinel.conf` (filter), `ssh-sentinel.local.example` (jail) |
| `TESTING.md` | the VM test runbook |

Files on a server (Linux):

| Path | Purpose | Owner / mode |
|---|---|---|
| `/usr/local/bin/ssh-sentinel` | the binary | `root 0755` |
| `/etc/ssh-sentinel/config.json` | backend base URL, server token, mode, timeouts | `root 0600` |
| `/etc/ssh-sentinel/breakglass` | break-glass whitelist, one username per line, `#` comments, hand-edited only, never synced. Ignored unless owned by root and writable by root only | `root 0600` |
| `/var/lib/ssh-sentinel/whitelist.json` | cache of the backend whitelist for this server, with expiry, written by `sync` | `root 0600` |
| `/etc/fail2ban/filter.d/ssh-sentinel.conf` | fail2ban filter, copied by the installer when fail2ban is present | `root 0644` |

## Configuration

`/etc/ssh-sentinel/config.json`, JSON because Go parses it with the standard library. Unknown keys are rejected, so a typo fails at install time instead of being ignored.

```json
{
  "backend_url": "https://api.example.com",
  "server_token": "the token printed by the enrollment script",
  "mode": "enforce",
  "timeout_seconds": 30,
  "hostname": "",
  "allow_http": false,
  "watch_poll_seconds": 2
}
```

| Key | Meaning |
|---|---|
| `backend_url` | base URL of the backend, https, no trailing slash. Required |
| `server_token` | this server's bearer token. Required |
| `mode` | `enforce` (default) or `notify` |
| `timeout_seconds` | whole budget of one access request, connection included, 1 to 120. Default 30 |
| `hostname` | name reported to the backend; empty means the OS hostname |
| `allow_http` | accept an `http://` backend URL. Lab use only |
| `watch_poll_seconds` | Windows watcher poll interval. Default 2 |

`SSH_SENTINEL_CONFIG=/path/config.json` overrides the config location for manual runs. The break-glass file and the cache stay at their fixed paths, so a missing or broken config never disables them; the backend then counts as unreachable.

PAM lines written by the installer, each preceded by the marker `# ssh-sentinel (managed by install.sh)` and inserted before the first include of the account stack (`@include common-account` on Debian and Ubuntu, `account include password-auth` or `system-auth` on the RHEL family). Before, not after: the RHEL includes contain `account sufficient pam_localuser.so`, and a sufficient module that succeeds ends the stack, so a line placed after the include would be skipped for local users. The installer backs each file up to `<file>.ssh-sentinel.bak` first.

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

## Install

On the server, as root, with the Linux binary and the `scripts/` folder copied next to each other:

```bash
sudo ./scripts/install.sh --backend-url https://api.example.com --token "$TOKEN" --mode notify
```

What it does, in order:

- copies the binary to `/usr/local/bin` and runs `ssh-sentinel version` before touching PAM,
- writes `config.json` and an empty break-glass file under `/etc/ssh-sentinel`,
- inserts the two PAM lines, with a backup of each file,
- installs and starts the sync timer (every 5 minutes) and runs a first sync,
- copies the fail2ban filter and the jail example when fail2ban is present,
- warns when SELinux is enforcing.

Re-running is safe. `--dry-run` prints every step instead of doing it. `--uninstall` removes the PAM lines, the timer and the binary; add `--purge` to remove the config, the break-glass file and the cache too. The token can be passed as `SSH_SENTINEL_TOKEN` to keep it out of the shell history.

Start in notify mode, watch `journalctl -t ssh-sentinel -f` and the history for a while, then set `"mode": "enforce"` in `config.json`. No restart needed: the agent reads the config on every call.

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

Add `-X main.version=1.0.0` to the `-ldflags` value to stamp a version (`ssh-sentinel version` prints it). A darwin/arm64 build is one more line and worth keeping in the release script, but nothing is tested on macOS.

## Windows

Build `ssh-sentinel-windows-amd64.exe`, copy it to `C:\Program Files\ssh-sentinel\ssh-sentinel.exe` and write `C:\ProgramData\ssh-sentinel\config.json` with the same keys as on Linux (`mode` is ignored: Windows always reports in notify mode). Then, in an administrator PowerShell:

```powershell
# register the event source once, so the Application log renders the lines cleanly
reg add "HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ssh-sentinel" /v EventMessageFile /t REG_EXPAND_SZ /d "%SystemRoot%\System32\EventCreate.exe" /f
reg add "HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ssh-sentinel" /v TypesSupported /t REG_DWORD /d 7 /f

# test: log in over SSH, then report the logins of the last five minutes
& "C:\Program Files\ssh-sentinel\ssh-sentinel.exe" watch --once

# run at boot as SYSTEM
schtasks /Create /TN ssh-sentinel /SC ONSTART /RU SYSTEM /TR "\"C:\Program Files\ssh-sentinel\ssh-sentinel.exe\" watch" /F
schtasks /Run /TN ssh-sentinel
```

The watcher polls the `OpenSSH/Operational` channel with `wevtutil` every `watch_poll_seconds` and sends one notify-mode request per accepted login. Decisions land in the Application log under the source `ssh-sentinel`, event id 100. `--channel` points it at another channel if OpenSSH logs elsewhere.

## Test

```powershell
go vet ./...
go test ./...
bash scripts/install_test.sh
```

Unit tests cover the decision table row by row against a fake backend (Go's `httptest` package) and a fake clock for cache expiry, the exact request bodies of the contract, the log line, the config validation and the event-log parser, so they run on Windows without PAM. `install_test.sh` runs the PAM insertion against Debian and RHEL layouts in a temp directory.

End-to-end tests run on a throwaway VM only (Multipass, VirtualBox or a cloud instance). Never on a production server. The full runbook is `TESTING.md`; in short:

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
- In notify mode the backend call is capped at 10 s, so a server in rollout never waits the full budget for a backend that is down.
- Cache expiry uses the server clock. Keep NTP running.
- macOS: Apple does not ship `pam_exec`. FreeBSD has one for OpenPAM, which macOS uses, so a port is the likely route when the time comes. Not scheduled.
