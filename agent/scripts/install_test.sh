#!/usr/bin/env bash
# Tests the PAM insertion and removal of install.sh against the four known layouts without
# touching the real system: SSH_SENTINEL_ROOT points every path at a temp dir.
# Run from anywhere: bash scripts/install_test.sh
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export SSH_SENTINEL_ROOT="$TMP"
# shellcheck source=install.sh
source "$HERE/install.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
passed=0
ok() { passed=$((passed + 1)); printf 'ok  %s\n' "$*"; }

mkdir -p "$TMP/etc/pam.d"

# check_insert NAME ORIGINAL INCLUDE_LINE LINE: inserts twice, checks position and count,
# removes, checks the original is back byte for byte.
check_insert() {
  local name="$1" original="$2" include="$3" line="$4"
  local file="$TMP/etc/pam.d/$name" n
  printf '%s' "$original" >"$file"
  printf '%s' "$original" >"$TMP/$name.orig"
  pam_insert "$file" "$line" >/dev/null
  pam_insert "$file" "$line" >/dev/null
  [[ "$(grep -cxF "$MARKER" "$file")" -eq 1 ]] || fail "$name: expected exactly one marker"
  [[ -f "$file.ssh-sentinel.bak" ]] || fail "$name: no backup file"
  cmp -s "$file.ssh-sentinel.bak" "$TMP/$name.orig" || fail "$name: backup differs from the original"
  n="$(grep -nxF "$MARKER" "$file" | cut -d: -f1)"
  [[ "$(sed -n "$((n + 1))p" "$file")" == "$line" ]] || fail "$name: the PAM line does not follow the marker"
  [[ "$(sed -n "$((n + 2))p" "$file")" == "$include" ]] || fail "$name: the line is not right before '$include'"
  pam_remove "$file" >/dev/null
  cmp -s "$file" "$TMP/$name.orig" || fail "$name: removal did not restore the original"
  pam_remove "$file" >/dev/null
  ok "$name"
}

DEBIAN_SSHD=$'@include common-auth\naccount    required     pam_nologin.so\n@include common-account\nsession    required     pam_loginuid.so\n@include common-session\n'
RHEL_SSHD=$'auth       substack     password-auth\nauth       include      postlogin\naccount    required     pam_sepermit.so\naccount    required     pam_nologin.so\naccount    include      password-auth\npassword   include      password-auth\nsession    required     pam_loginuid.so\n'
DEBIAN_SUDO=$'@include common-auth\n@include common-account\n@include common-session-noninteractive\n'
RHEL_SUDO=$'auth       include      system-auth\naccount    include      system-auth\npassword   include      system-auth\nsession    include      system-auth\n'

check_insert sshd-debian "$DEBIAN_SSHD" "@include common-account" "$SSHD_LINE"
check_insert sshd-rhel   "$RHEL_SSHD"   "account    include      password-auth" "$SSHD_LINE"
check_insert sudo-debian "$DEBIAN_SUDO" "@include common-account" "$SUDO_LINE"
check_insert sudo-rhel   "$RHEL_SUDO"   "account    include      system-auth" "$SUDO_LINE"

# No include line at all: before the first account line.
file="$TMP/etc/pam.d/plain"
printf 'auth required pam_unix.so\naccount required pam_unix.so\nsession required pam_unix.so\n' >"$file"
pam_insert "$file" "$SSHD_LINE" >/dev/null
[[ "$(sed -n '2p' "$file")" == "$MARKER" && "$(sed -n '4p' "$file")" == "account required pam_unix.so" ]] || fail "plain: not before the first account line"
ok "no include line: before the first account line"

# No account line at all: appended.
file="$TMP/etc/pam.d/noaccount"
printf 'auth required pam_unix.so\n' >"$file"
pam_insert "$file" "$SSHD_LINE" >/dev/null
[[ "$(tail -n 1 "$file")" == "$SSHD_LINE" ]] || fail "noaccount: not appended"
ok "no account line: appended at the end"

# Family detection reads the fake os-release under the root.
printf 'ID=rocky\nID_LIKE="rhel centos fedora"\n' >"$TMP/etc/os-release"
detect_family >/dev/null
[[ "$FAMILY" == rhel ]] || fail "family: expected rhel, got $FAMILY"
printf 'ID=ubuntu\nID_LIKE=debian\n' >"$TMP/etc/os-release"
detect_family >/dev/null
[[ "$FAMILY" == debian ]] || fail "family: expected debian, got $FAMILY"
ok "distro family detection"

printf 'all %d checks passed\n' "$passed"
