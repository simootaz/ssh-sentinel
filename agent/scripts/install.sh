#!/usr/bin/env bash
# ssh-sentinel installer for Linux, Debian and RHEL families.
#
# Installs the binary, writes the config, inserts the two PAM lines, enables the sync timer
# and, when fail2ban is present, drops the filter. Safe to run again. --uninstall reverts it.
#
# Usage:
#   install.sh --backend-url URL --token TOKEN [--mode enforce|notify] [--binary PATH]
#              [--hostname NAME] [--allow-http] [--dry-run]
#   install.sh --uninstall [--purge]
#   install.sh --help
#
# Environment: SSH_SENTINEL_BACKEND_URL and SSH_SENTINEL_TOKEN replace --backend-url and
# --token, so the token does not end up in the shell history. SSH_SENTINEL_ROOT prefixes every
# path the script touches; it exists for install_test.sh and must stay empty on a real server.
set -euo pipefail

ROOT="${SSH_SENTINEL_ROOT:-}"
BIN_DIR="$ROOT/usr/local/bin"
CONF_DIR="$ROOT/etc/ssh-sentinel"
STATE_DIR="$ROOT/var/lib/ssh-sentinel"
PAM_DIR="$ROOT/etc/pam.d"
UNIT_DIR="$ROOT/etc/systemd/system"
F2B_DIR="$ROOT/etc/fail2ban"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The path written into PAM is the real one, never prefixed.
BINARY_PATH="/usr/local/bin/ssh-sentinel"
MARKER="# ssh-sentinel (managed by install.sh)"
SSHD_LINE="account required pam_exec.so quiet $BINARY_PATH check"
SUDO_LINE="account required pam_exec.so quiet $BINARY_PATH check --context=sudo"

DRY_RUN=0
BACKEND_URL="${SSH_SENTINEL_BACKEND_URL:-}"
TOKEN="${SSH_SENTINEL_TOKEN:-}"
MODE="enforce"
BINARY=""
HOSTNAME_OVERRIDE=""
ALLOW_HTTP=0
UNINSTALL=0
PURGE=0
FAMILY=""

log()  { printf '==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# run CMD...: executes the command, or prints it under --dry-run.
run() {
  if [[ $DRY_RUN -eq 1 ]]; then printf '[dry-run] %s\n' "$*"; else "$@"; fi
}

# write_file PATH MODE < content: writes stdin to PATH, or prints the intent under --dry-run.
write_file() {
  local path="$1" mode="$2"
  if [[ $DRY_RUN -eq 1 ]]; then
    printf '[dry-run] write %s (mode %s)\n' "$path" "$mode"
    cat >/dev/null
    return 0
  fi
  mkdir -p "$(dirname "$path")"
  cat >"$path"
  chmod "$mode" "$path"
}

usage() {
  sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --backend-url) BACKEND_URL="$2"; shift 2 ;;
      --token)       TOKEN="$2"; shift 2 ;;
      --mode)        MODE="$2"; shift 2 ;;
      --binary)      BINARY="$2"; shift 2 ;;
      --hostname)    HOSTNAME_OVERRIDE="$2"; shift 2 ;;
      --allow-http)  ALLOW_HTTP=1; shift ;;
      --dry-run)     DRY_RUN=1; shift ;;
      --uninstall)   UNINSTALL=1; shift ;;
      --purge)       PURGE=1; shift ;;
      -h|--help)     usage; exit 0 ;;
      *)             die "unknown argument: $1 (see --help)" ;;
    esac
  done
}

require_root() {
  [[ -n "$ROOT" || $DRY_RUN -eq 1 ]] && return 0
  [[ "$(id -u)" -eq 0 ]] || die "run this script as root"
}

# detect_family sets FAMILY to debian or rhel from /etc/os-release, falling back to the
# package manager present. Both families get the same PAM lines; only the log file and the
# include line names differ.
detect_family() {
  local id="" like="" rel="$ROOT/etc/os-release"
  if [[ -r "$rel" ]]; then
    id="$(awk -F= '$1 == "ID" { gsub(/"/, "", $2); print $2 }' "$rel")"
    like="$(awk -F= '$1 == "ID_LIKE" { gsub(/"/, "", $2); print $2 }' "$rel")"
  fi
  case " $id $like " in
    *" debian "*|*" ubuntu "*) FAMILY=debian ;;
    *" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" fedora "*) FAMILY=rhel ;;
    *)
      if command -v apt-get >/dev/null 2>&1; then FAMILY=debian
      elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then FAMILY=rhel
      else die "cannot tell the distro family (no apt-get, dnf or yum); supported: Debian, Ubuntu, RHEL, Rocky, AlmaLinux"
      fi ;;
  esac
  log "distro family: $FAMILY"
}

# pam_insert FILE LINE: puts the marker and LINE before the first line that pulls another
# file into the account stack (Debian: "@include common-account"; RHEL: "account include
# password-auth" or "account substack ..."). Position matters: the RHEL family's password-auth
# and system-auth contain "account sufficient pam_localuser.so", and a sufficient module that
# succeeds ends the account stack, so a line placed after the include would never run for
# local users. Placed before it, with "required", a deny is recorded and the stack fails as
# intended. Debian's common-account works either way. Falls back to the first "account" line,
# then to the end of the file. The file is rewritten through a temp file, so it is never left
# half-written, and backed up once to FILE.ssh-sentinel.bak.
pam_insert() {
  local file="$1" line="$2" tmp
  [[ -f "$file" ]] || die "$file not found"
  if grep -qxF "$MARKER" "$file"; then
    log "$file already has the ssh-sentinel line"
    return 0
  fi
  if [[ $DRY_RUN -eq 1 ]]; then
    printf '[dry-run] insert into %s before the account include: %s\n' "$file" "$line"
    return 0
  fi
  [[ -f "$file.ssh-sentinel.bak" ]] || cp -p "$file" "$file.ssh-sentinel.bak"
  tmp="$(mktemp "$file.XXXXXX")"
  awk -v marker="$MARKER" -v line="$line" '
    function is_include(l) {
      return l ~ /^@include[ \t]+common-account/ || l ~ /^account[ \t]+(include|substack)[ \t]/
    }
    { lines[NR] = $0 }
    END {
      at = 0
      for (i = 1; i <= NR; i++) if (is_include(lines[i])) { at = i; break }
      if (at == 0) for (i = 1; i <= NR; i++) if (lines[i] ~ /^account[ \t]/) { at = i; break }
      for (i = 1; i <= NR; i++) {
        if (i == at) { print marker; print line }
        print lines[i]
      }
      if (at == 0) { print marker; print line }
    }' "$file" >"$tmp"
  chmod --reference="$file" "$tmp"
  mv "$tmp" "$file"
  log "$file: ssh-sentinel line inserted (backup: $file.ssh-sentinel.bak)"
}

# pam_remove FILE: drops the marker and the line after it. Safe when nothing is there.
pam_remove() {
  local file="$1" tmp
  [[ -f "$file" ]] || return 0
  grep -qxF "$MARKER" "$file" || return 0
  if [[ $DRY_RUN -eq 1 ]]; then
    printf '[dry-run] remove the ssh-sentinel line from %s\n' "$file"
    return 0
  fi
  tmp="$(mktemp "$file.XXXXXX")"
  awk -v marker="$MARKER" '
    $0 == marker { skip = 1; next }
    skip == 1    { skip = 0; next }
    { print }' "$file" >"$tmp"
  chmod --reference="$file" "$tmp"
  mv "$tmp" "$file"
  log "$file: ssh-sentinel line removed"
}

install_binary() {
  if [[ -z "$BINARY" ]]; then
    local arch
    case "$(uname -m)" in
      x86_64|amd64)  arch=amd64 ;;
      aarch64|arm64) arch=arm64 ;;
      *) die "unsupported architecture $(uname -m); pass --binary PATH" ;;
    esac
    BINARY="$SCRIPT_DIR/ssh-sentinel-linux-$arch"
    [[ -f "$BINARY" ]] || BINARY="$PWD/ssh-sentinel-linux-$arch"
  fi
  [[ -f "$BINARY" ]] || die "binary not found: $BINARY (build it on the dev machine, see agent/README.md, or pass --binary)"
  run mkdir -p "$BIN_DIR"
  run cp "$BINARY" "$BIN_DIR/ssh-sentinel"
  run chmod 0755 "$BIN_DIR/ssh-sentinel"
  [[ -z "$ROOT" ]] && run chown root:root "$BIN_DIR/ssh-sentinel"
  # Prove the binary runs on this machine before PAM depends on it.
  if [[ $DRY_RUN -eq 0 && -z "$ROOT" ]]; then
    "$BIN_DIR/ssh-sentinel" version >/dev/null || die "the binary does not run on this machine"
  fi
  log "binary installed at $BIN_DIR/ssh-sentinel"
}

write_config() {
  run mkdir -p "$CONF_DIR" "$STATE_DIR"
  run chmod 0700 "$CONF_DIR" "$STATE_DIR"
  local conf="$CONF_DIR/config.json"
  if [[ -f "$conf" && -z "$BACKEND_URL" ]]; then
    log "keeping the existing $conf"
  else
    [[ -n "$BACKEND_URL" ]] || die "--backend-url (or SSH_SENTINEL_BACKEND_URL) is required"
    [[ -n "$TOKEN" ]] || die "--token (or SSH_SENTINEL_TOKEN) is required"
    [[ "$MODE" == enforce || "$MODE" == notify ]] || die "--mode must be enforce or notify"
    local allow=false hostline=""
    [[ $ALLOW_HTTP -eq 1 ]] && allow=true
    [[ -n "$HOSTNAME_OVERRIDE" ]] && hostline=$'\n'"  \"hostname\": \"$HOSTNAME_OVERRIDE\","
    write_file "$conf" 0600 <<EOF
{
  "backend_url": "$BACKEND_URL",
  "server_token": "$TOKEN",
  "mode": "$MODE",$hostline
  "timeout_seconds": 30,
  "allow_http": $allow
}
EOF
    log "config written to $conf (mode $MODE)"
  fi
  local bg="$CONF_DIR/breakglass"
  if [[ ! -f "$bg" ]]; then
    write_file "$bg" 0600 <<'EOF'
# ssh-sentinel break-glass list: one username per line, '#' starts a comment.
# Users listed here are allowed without asking the backend, for ssh and sudo alike.
# Keep it to one or two accounts. The file must stay owned by root with mode 0600,
# otherwise the agent ignores it.
EOF
    log "empty break-glass file created at $bg"
  fi
  warn "add at least one break-glass user to $bg before you rely on this server"
  warn "keep a root shell open while testing: a broken PAM config locks SSH out"
}

install_pam() {
  pam_insert "$PAM_DIR/sshd" "$SSHD_LINE"
  pam_insert "$PAM_DIR/sudo" "$SUDO_LINE"
}

# The timer refreshes the whitelist cache every five minutes. It is what keeps whitelisted
# users working when the backend is unreachable.
install_timer() {
  write_file "$UNIT_DIR/ssh-sentinel-sync.service" 0644 <<EOF
[Unit]
Description=ssh-sentinel whitelist cache refresh
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=$BINARY_PATH sync
EOF
  write_file "$UNIT_DIR/ssh-sentinel-sync.timer" 0644 <<'EOF'
[Unit]
Description=ssh-sentinel whitelist cache refresh, every 5 minutes

[Timer]
OnBootSec=1min
OnUnitActiveSec=5min
Persistent=true

[Install]
WantedBy=timers.target
EOF
  if [[ -n "$ROOT" || $DRY_RUN -eq 1 ]]; then
    printf '[skipped] systemctl daemon-reload; systemctl enable --now ssh-sentinel-sync.timer\n'
    return 0
  fi
  systemctl daemon-reload
  systemctl enable --now ssh-sentinel-sync.timer
  log "sync timer enabled (systemctl list-timers ssh-sentinel-sync.timer)"
  if "$BIN_DIR/ssh-sentinel" sync; then
    log "first sync done"
  else
    warn "first sync failed; the timer retries every 5 minutes (is the backend reachable and the token valid?)"
  fi
}

install_fail2ban() {
  if [[ ! -d "$F2B_DIR" ]]; then
    log "fail2ban is not installed, skipping the filter"
    return 0
  fi
  local src="$SCRIPT_DIR/fail2ban"
  if [[ ! -f "$src/ssh-sentinel.conf" ]]; then
    warn "fail2ban filter not found next to the script ($src), skipping"
    return 0
  fi
  run mkdir -p "$F2B_DIR/filter.d" "$F2B_DIR/jail.d"
  run cp "$src/ssh-sentinel.conf" "$F2B_DIR/filter.d/ssh-sentinel.conf"
  run chmod 0644 "$F2B_DIR/filter.d/ssh-sentinel.conf"
  run cp "$src/ssh-sentinel.local.example" "$F2B_DIR/jail.d/ssh-sentinel.local.example"
  run chmod 0644 "$F2B_DIR/jail.d/ssh-sentinel.local.example"
  log "fail2ban filter installed. To enable the jail:"
  log "  cp $F2B_DIR/jail.d/ssh-sentinel.local.example $F2B_DIR/jail.d/ssh-sentinel.local && systemctl reload fail2ban"
}

warn_selinux() {
  if command -v getenforce >/dev/null 2>&1 && [[ "$(getenforce 2>/dev/null)" == "Enforcing" ]]; then
    warn "SELinux is enforcing. After the first login test run: ausearch -m avc -ts recent"
    warn "The sshd and sudo contexts may need a policy addition to allow the outbound HTTPS call."
  fi
}

summary() {
  cat <<EOF

ssh-sentinel is installed (mode: $MODE).

  binary       $BIN_DIR/ssh-sentinel
  config       $CONF_DIR/config.json        (edit and save, no restart needed)
  break-glass  $CONF_DIR/breakglass         (add one user now)
  cache        $STATE_DIR/whitelist.json    (refreshed by the sync timer)
  PAM          $PAM_DIR/sshd, $PAM_DIR/sudo (backups: *.ssh-sentinel.bak)

Test without logging in:
  PAM_USER=alice PAM_RHOST=203.0.113.42 PAM_TTY=ssh $BINARY_PATH check; echo \$?
Watch the decisions:
  journalctl -t ssh-sentinel -f
Switch mode: set "mode" to "enforce" or "notify" in config.json.
Uninstall:   $0 --uninstall [--purge]
EOF
}

uninstall() {
  log "removing the PAM lines"
  pam_remove "$PAM_DIR/sshd"
  pam_remove "$PAM_DIR/sudo"
  if [[ -z "$ROOT" && $DRY_RUN -eq 0 ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now ssh-sentinel-sync.timer 2>/dev/null || true
  fi
  run rm -f "$UNIT_DIR/ssh-sentinel-sync.service" "$UNIT_DIR/ssh-sentinel-sync.timer"
  if [[ -z "$ROOT" && $DRY_RUN -eq 0 ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
  fi
  run rm -f "$BIN_DIR/ssh-sentinel"
  run rm -f "$F2B_DIR/filter.d/ssh-sentinel.conf" "$F2B_DIR/jail.d/ssh-sentinel.local.example"
  if [[ $PURGE -eq 1 ]]; then
    run rm -rf "$CONF_DIR" "$STATE_DIR"
    log "config, break-glass file and cache removed"
  else
    log "kept $CONF_DIR and $STATE_DIR (use --purge to remove them)"
  fi
  log "uninstalled"
}

main() {
  parse_args "$@"
  require_root
  if [[ $UNINSTALL -eq 1 ]]; then
    uninstall
    return 0
  fi
  [[ -f "$PAM_DIR/sshd" ]] || die "$PAM_DIR/sshd not found; is this a Linux server with OpenSSH and PAM?"
  detect_family
  install_binary
  write_config
  install_pam
  install_timer
  install_fail2ban
  warn_selinux
  summary
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
