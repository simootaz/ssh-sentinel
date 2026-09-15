# Testing the agent

Runbook for the end-to-end test of `ssh-sentinel` on Linux VMs and on a Windows box. It follows the seven steps of `README.md` and adds the checks that need no login at all.

## Rules

- Throwaway VMs only. Never a production server, never a server you cannot reach through a console.
- Before touching PAM, open a root shell on the VM and keep it open until the end. A broken PAM config locks SSH out; that shell, or the console, is the way back.
- Snapshot the VM after the OS install and before the first `install.sh`.
- Start every server in notify mode. Switch to enforce only after a login showed up in history.

## Prerequisites

- The backend base URL and a server token from the enrollment script, one token per VM.
- A phone with the app registered on that backend.
- The Linux binary built on the dev machine, from `agent\` in PowerShell:

```powershell
$env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -trimpath -ldflags "-s -w" -o bin\ssh-sentinel-linux-amd64 .\cmd\ssh-sentinel
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```

- An Ubuntu VM through Multipass on the dev machine, and a Rocky VM through VirtualBox or a cloud instance (Multipass ships Ubuntu images only):

```powershell
multipass launch --name sentinel-ubuntu 24.04
multipass transfer bin\ssh-sentinel-linux-amd64 sentinel-ubuntu:/home/ubuntu/
multipass transfer -r scripts sentinel-ubuntu:/home/ubuntu/
multipass shell sentinel-ubuntu
```

For Rocky, `scp` the same two items to the VM. The binary and the `scripts` folder must sit next to each other.

- Inside the VM, a second user to log in with (`sudo adduser tester` on Ubuntu, `sudo useradd -m tester && sudo passwd tester` on Rocky) and the sshd password authentication enabled for that user, or its public key installed.
- fail2ban for step 7: `sudo apt install fail2ban` or `sudo dnf install epel-release fail2ban`.

Watch the decisions in a second terminal on the VM during every step:

```bash
sudo journalctl -t ssh-sentinel -f
```

## The seven steps

### 1. Install in notify mode

```bash
sudo ./scripts/install.sh --backend-url https://api.example.com --token "$TOKEN" --mode notify
```

Expected: `distro family: debian` (or `rhel`), the binary version, the config written, the two PAM lines inserted with their backups, the timer enabled, `synced 0 whitelist entries` (or more), and the fail2ban filter installed when fail2ban is present. On Rocky with SELinux enforcing, a warning. If the first sync fails with `backend unreachable`, check the URL and the token before going on: the timer will keep retrying every five minutes.

Then add the tester as a break-glass user for now, so the enforce steps cannot lock you out while you learn the flow:

```bash
echo tester | sudo tee -a /etc/ssh-sentinel/breakglass
```

You will remove it again in step 4.

### 2. Keep a root shell open

Already done if you followed the rules. Also check that you can still open a new SSH session as your admin user right now, before enforce mode.

### 3. First login, notify mode

From the dev machine, `ssh tester@<vm ip>`. Expected: the login opens immediately, the phone shows an informational notification without buttons, the journal shows

```
decision=allow context=ssh user=tester rhost=<your ip> host=<vm> reason=notify request=<id>
```

Wait: the break-glass entry from step 1 makes this line say `reason=breakglass` instead. Either is fine here; what matters is that the login was immediate. Now switch to enforce mode and remove the break-glass entry:

```bash
sudo sed -i 's/"mode": "notify"/"mode": "enforce"/' /etc/ssh-sentinel/config.json
sudo sed -i '/^tester$/d' /etc/ssh-sentinel/breakglass
```

No restart is needed. If the phone showed nothing, check the history in the app first: a `notified` row means the push side is the problem, no row means the request never reached the backend.

### 4. Enforce: ssh, then sudo

`ssh tester@<vm ip>` again. Expected: the SSH client waits, the phone shows the request with Deny, Approve and Always allow, and the countdown runs. Tap Approve: the session opens within a second. Journal:

```
decision=allow context=ssh user=tester rhost=<your ip> host=<vm> reason=admin request=<id>
```

In that session run `sudo -i`. Expected: a second push marked sudo, showing the command (`sudo -i`) and no source IP. Tap Approve, the root shell opens. Journal:

```
decision=allow context=sudo user=tester rhost=- host=<vm> reason=admin request=<id>
```

Then repeat the login and tap Deny: the SSH client prints `Permission denied` or closes, and the journal shows `decision=deny ... reason=admin`. Let one request expire without tapping: after about 30 s the login is refused with `reason=timeout`.

If the login waits and nothing reaches the phone, look at `sudo journalctl -u ssh -n 20` (or `-u sshd`) and at the backend history. If the login is refused at once with `reason=rejected`, the token in `config.json` is wrong. On Rocky, a `reason=unreachable` with a working backend usually means SELinux: `sudo ausearch -m avc -ts recent`.

### 5. Always allow with a TTL, then sync

Run `sudo -i` again and tap Always allow, choose 1 hour. Expected: the shell opens, the journal shows `reason=admin`. Run `sudo -i` a third time: no push, immediate shell, journal shows `reason=whitelist` (the backend answered from its whitelist). Now refresh the cache and read it:

```bash
sudo ssh-sentinel sync
sudo cat /var/lib/ssh-sentinel/whitelist.json
```

Expected: `synced 1 whitelist entries to /var/lib/ssh-sentinel/whitelist.json` and an entry `{"username": "tester", "context": "sudo", "expires_at": "<one hour from now, UTC>"}`. The timer does the same every five minutes: `systemctl list-timers ssh-sentinel-sync.timer`.

### 6. Backend unreachable

Point the config at an address that answers nothing, and add a break-glass user:

```bash
sudo sed -i 's#"backend_url": ".*"#"backend_url": "https://127.0.0.1:9"#' /etc/ssh-sentinel/config.json
echo ubuntu | sudo tee -a /etc/ssh-sentinel/breakglass     # or your admin user on Rocky
date -u                                                     # cache expiry uses this clock
```

Expected, each within a couple of seconds:

- `ssh ubuntu@<vm ip>`: immediate login, journal `reason=breakglass request=-`.
- `ssh tester@<vm ip>` then `sudo -i`: the ssh login is refused (`context=ssh ... reason=unreachable`, no ssh entry in the cache) but if you log in from the open root shell with `su - tester` and run `sudo -i`, it is allowed with `context=sudo ... reason=cache`, as long as the entry from step 5 has not expired.
- `ssh someone-else@<vm ip>`: refused, `reason=unreachable request=-`.

Restore the real backend URL afterwards and remove the extra break-glass user.

### 7. fail2ban

With fail2ban installed, enable the jail:

```bash
sudo cp /etc/fail2ban/jail.d/ssh-sentinel.local.example /etc/fail2ban/jail.d/ssh-sentinel.local
sudo systemctl reload fail2ban
sudo fail2ban-regex systemd-journal /etc/fail2ban/filter.d/ssh-sentinel.conf
```

From a second client machine, log in three times as `tester` and tap Deny each time. Expected: `sudo fail2ban-client status ssh-sentinel` lists that client's IP under `Banned IP list`, and its fourth connection attempt is dropped before sshd answers. `sudo fail2ban-client set ssh-sentinel unbanip <ip>` lifts it. The backend's auto-block fires at the same count fleet-wide; check the blocked IPs screen in the app.

## Checks that need no login

All of these run as root on the VM and print the exit code: `0` allow, `1` deny.

Run the hook by hand with the variables pam_exec would set:

```bash
PAM_USER=tester PAM_RHOST=203.0.113.42 PAM_TTY=ssh /usr/local/bin/ssh-sentinel check; echo $?
PAM_USER=tester PAM_TTY=/dev/pts/0 /usr/local/bin/ssh-sentinel check --context=sudo; echo $?
```

In enforce mode the first command waits for the phone; in notify mode both return `0` at once and the phone gets a notification without buttons.

Break-glass with the backend down: set `backend_url` to `https://127.0.0.1:9`, add `tester` to `/etc/ssh-sentinel/breakglass`, run the first command again: `0` within a second and `reason=breakglass` in the journal. Then `chmod 644 /etc/ssh-sentinel/breakglass` and run it again: `1`, because the agent ignores a break-glass file that is not root-only. Put the mode back to `0600`.

Cache with expiry, backend still down. Write the cache by hand with one expired and one valid entry (dates are UTC, RFC 3339):

```bash
cat > /var/lib/ssh-sentinel/whitelist.json <<'EOF'
{
  "synced_at": "2026-09-14T20:00:00Z",
  "entries": [
    {"username": "old", "context": "ssh", "expires_at": "2020-01-01T00:00:00Z"},
    {"username": "tester", "context": "ssh", "expires_at": null},
    {"username": "tester", "context": "sudo", "expires_at": "2030-01-01T00:00:00Z"}
  ]
}
EOF
chmod 600 /var/lib/ssh-sentinel/whitelist.json
PAM_USER=old    PAM_RHOST=203.0.113.42 PAM_TTY=ssh /usr/local/bin/ssh-sentinel check; echo $?   # 1, expired
PAM_USER=tester PAM_RHOST=203.0.113.42 PAM_TTY=ssh /usr/local/bin/ssh-sentinel check; echo $?   # 0, reason=cache
PAM_USER=tester PAM_TTY=/dev/pts/0 /usr/local/bin/ssh-sentinel check --context=sudo; echo $?    # 0, reason=cache
PAM_USER=nobody PAM_RHOST=203.0.113.42 PAM_TTY=ssh /usr/local/bin/ssh-sentinel check; echo $?   # 1, reason=unreachable
```

The next `ssh-sentinel sync` overwrites this file with the real list.

Missing config: `mv /etc/ssh-sentinel/config.json /tmp/` and run the tester command: `1` for a user with no cache entry, `0` for a cached or break-glass user, plus a `config file not found` line on stderr. Move the file back.

Scripts, on the dev machine in Git Bash or on the VM:

```bash
bash -n scripts/install.sh
bash scripts/install_test.sh
sudo SSH_SENTINEL_ROOT=/tmp/fake ./scripts/install.sh --dry-run --backend-url https://x --token t --binary ./ssh-sentinel-linux-amd64
```

`install_test.sh` prints `all 7 checks passed`. The dry run lists every action without doing any of them.

## Windows

Build `ssh-sentinel-windows-amd64.exe` (README, Build), copy it to `C:\Program Files\ssh-sentinel\` on a Windows VM with OpenSSH Server installed, and write `C:\ProgramData\ssh-sentinel\config.json` with the backend URL and the token. In an administrator PowerShell:

```powershell
reg add "HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ssh-sentinel" /v EventMessageFile /t REG_EXPAND_SZ /d "%SystemRoot%\System32\EventCreate.exe" /f
reg add "HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ssh-sentinel" /v TypesSupported /t REG_DWORD /d 7 /f
```

Log in to the box over SSH from another machine, then run:

```powershell
& "C:\Program Files\ssh-sentinel\ssh-sentinel.exe" watch --once
```

Expected: `reported 1 logins`, an informational push on the phone, a `notified` row in history, and in Event Viewer, Windows Logs, Application, source `ssh-sentinel`, event id 100, the line `decision=allow context=ssh user=<you> rhost=<ip> host=<box> reason=notify request=<id>`. If it reports `0 logins`, check that `wevtutil qe OpenSSH/Operational /c:3 /rd:true /f:text` shows an `Accepted` line for your login; the OpenSSH log level may need `LogLevel VERBOSE` in `C:\ProgramData\ssh\sshd_config` followed by `Restart-Service sshd`.

Then register the watcher at boot and start it:

```powershell
schtasks /Create /TN ssh-sentinel /SC ONSTART /RU SYSTEM /TR "\"C:\Program Files\ssh-sentinel\ssh-sentinel.exe\" watch" /F
schtasks /Run /TN ssh-sentinel
```

Log in again: the push arrives within the poll interval (2 s by default). Windows never blocks a login.

## Cleanup

```bash
sudo ./scripts/install.sh --uninstall --purge
```

Check that `/etc/pam.d/sshd` and `/etc/pam.d/sudo` match their `.ssh-sentinel.bak` copies, restore the snapshot, delete the VM (`multipass delete sentinel-ubuntu && multipass purge`), and delete the VM's server token from the backend.
