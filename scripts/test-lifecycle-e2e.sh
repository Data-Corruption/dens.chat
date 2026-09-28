#!/usr/bin/env bash

# Linux lifecycle e2e. Installs Dens through the rendered install.sh in fresh
# Incus system containers and runs the M0 flows on each distro (the steps are
# in scripts/test/lifecycle-guest.sh): install as a sudo user, pair a browser,
# set the password, back up, refuse another local user, reboot, update, add a
# second instance, restore, and uninstall. Each distro after the first
# restores the backup the previous one made, which covers restoring on
# another machine.
#
# Usage:
#   ./scripts/test-lifecycle-e2e.sh                          # every distro
#   ./scripts/test-lifecycle-e2e.sh --distros "debian arch"
#   ./scripts/test-lifecycle-e2e.sh --release-dir DIR        # a staged release
#   ./scripts/test-lifecycle-e2e.sh --backup FILE            # first distro restores FILE
#   KEEP_FAILED=true ./scripts/test-lifecycle-e2e.sh         # keep failed containers
#
# Without --release-dir the harness builds two unsigned fixture releases, one
# to install and a newer one to update to, and installs with
# APP_SKIP_VERIFY=true; signatures are covered by installing a real release.
# A --release-dir snapshot (root version pointer, install.sh, releases/) has
# no newer release, so the update step is skipped.
#
# Logs go to out/lifecycle-e2e-logs/<run>/: one file per distro, the backup
# each distro made, and a summary. --backup takes a backup made elsewhere,
# such as by the Windows harness, with the same test password.
#
# Needs a local Incus daemon (6.0 LTS runs on WSL kernels) with admin access,
# directly through incus-admin or through passwordless sudo, since some
# distros need privileged containers. Images come from the images: remote. A
# local alias dens-e2e/<distro> takes precedence, which saves downloads when
# iterating:
#   incus image copy images:debian/trixie local: --alias dens-e2e/debian-13

set -euo pipefail
# incus init reads instance config from a non-terminal stdin.
exec </dev/null

cd "$(dirname "$0")/.."

ALL_DISTROS="debian-12 debian-13 ubuntu-24.04 ubuntu-26.04 fedora-43 fedora-44 arch"
DISTROS=$ALL_DISTROS
SNAPSHOT=""
BACKUP=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --distros) DISTROS=$2; shift 2 ;;
    --release-dir) SNAPSHOT=$2; shift 2 ;;
    --backup) BACKUP=$2; shift 2 ;;
    *) printf "error: unknown argument '%s'\n" "$1" >&2; exit 1 ;;
  esac
done

case "${KEEP_FAILED:-false}" in
  true) KEEP_FAILED=true ;;
  false|"") KEEP_FAILED=false ;;
  *) echo "error: KEEP_FAILED must be true or false" >&2; exit 1 ;;
esac

# shellcheck source=test/incus.sh
source scripts/test/incus.sh
for distro in $DISTROS; do
  distro_spec "$distro" >/dev/null || { echo "error: unknown distro '$distro'" >&2; exit 1; }
done

incus_setup

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
RUN_LOG_DIR="$PWD/out/lifecycle-e2e-logs/$RUN_ID"
mkdir -p "$RUN_LOG_DIR"
HARNESS_DIR=$(mktemp -d)
RESULTS=()
# The container of the distro under test; distros run one at a time.
ACTIVE=""

cleanup() {
  if [[ -n "$ACTIVE" ]]; then
    if $KEEP_FAILED; then
      echo ">> Kept $ACTIVE for inspection: incus exec $ACTIVE -- bash"
    else
      "${INCUS[@]}" delete --force "$ACTIVE" >/dev/null 2>&1 || :
    fi
  fi
  rm -rf "$HARNESS_DIR"
}

summarize() {
  local status=$? line passed=0 failed=0
  trap - EXIT
  cleanup
  for line in "${RESULTS[@]}"; do
    case $line in
      PASS*) passed=$((passed + 1)) ;;
      *) failed=$((failed + 1)) ;;
    esac
  done
  {
    printf '\nLinux lifecycle e2e\n\n'
    for line in "${RESULTS[@]}"; do
      printf '  %s\n' "$line"
    done
    printf '\n%d passed, %d failed\n' "$passed" "$failed"
    printf 'Logs: %s\n' "$RUN_LOG_DIR"
  } | tee "$RUN_LOG_DIR/summary.txt"
  if [[ "$status" -eq 0 ]] && { [[ "$failed" -gt 0 ]] || [[ "$passed" -eq 0 ]]; }; then
    status=1
  fi
  exit "$status"
}
trap summarize EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Fixture releases ------------------------------------------------------------

RELEASE="$HARNESS_DIR/release"
if [[ -n "$SNAPSHOT" ]]; then
  [[ -f "$SNAPSHOT/version" && -f "$SNAPSHOT/install.sh" ]] ||
    { echo "error: $SNAPSHOT isn't a release snapshot" >&2; exit 1; }
  mkdir -p "$RELEASE"
  cp -a "$SNAPSHOT/." "$RELEASE/"
  V1=$(tr -d '\r\n' < "$RELEASE/version")
  V2=""
else
  case "$(uname -m)" in
    x86_64|amd64) target=linux-amd64 ;;
    aarch64|arm64) target=linux-arm64 ;;
    *) echo "error: unsupported host architecture $(uname -m)" >&2; exit 1 ;;
  esac
  echo ">> Building fixture releases ..."
  scripts/test/fixture-releases.sh "$RELEASE" "$target"
  V1=$(tr -d '\r\n' < "$RELEASE/version")
  V2=$(tr -d '\r\n' < "$RELEASE/next/version")
fi

# Containers ------------------------------------------------------------------

launch() {
  local distro=$1 name=$2
  launch_container "$distro" "$name" || return
  push_release "$name" "$RELEASE" || return
  "${INCUS[@]}" file push -q scripts/test/lifecycle-guest.sh "$name/root/lifecycle-guest.sh"
}

guest() {
  local name=$1 phase=$2
  timeout 1200 "${INCUS[@]}" exec "$name" --env "V1=$V1" --env "V2=$V2" -- \
    sh /root/lifecycle-guest.sh "$phase"
}

# run_distro DISTRO NAME PREVIOUS_BACKUP
run_distro() {
  local distro=$1 name=$2 previous=$3
  launch "$distro" "$name" || return
  if [[ -n "$previous" ]]; then
    "${INCUS[@]}" file push -q "$previous" "$name/root/previous.backup" || return
  fi
  guest "$name" install || return
  # Pull through stdout so this shell creates the file: incus may run under
  # sudo, and a root-owned 0600 copy is unreadable to the rest of the run.
  "${INCUS[@]}" file pull -q "$name/root/alice.backup" - > "$RUN_LOG_DIR/$distro.backup" || return
  echo ">> Rebooting $name"
  "${INCUS[@]}" restart --timeout 120 "$name" || return
  wait_for_boot "$name" || return
  guest "$name" after-reboot
}

previous=""
previous_distro=""
if [[ -n "$BACKUP" ]]; then
  [[ -f "$BACKUP" ]] || { echo "error: no backup at $BACKUP" >&2; exit 1; }
  previous=$(readlink -f "$BACKUP")
  previous_distro=${BACKUP##*/}
fi
for requested in $DISTROS; do
  read -r distro _ _ < <(distro_spec "$requested")
  name="dens-e2e-${distro//./-}-$$"
  log="$RUN_LOG_DIR/$distro.log"
  printf '\n==============================================================\n'
  printf '>> %s\n' "$distro"
  printf '==============================================================\n'
  ACTIVE=$name
  set +e
  run_distro "$distro" "$name" "$previous" 2>&1 | tee "$log"
  status=${PIPESTATUS[0]}
  set -e
  if [[ "$status" -eq 0 ]]; then
    RESULTS+=("PASS  $distro")
    previous="$RUN_LOG_DIR/$distro.backup"
    previous_distro=$distro
    "${INCUS[@]}" delete --force "$name" >/dev/null
  else
    note=""
    [[ -z "$previous_distro" ]] || note=" (restoring $previous_distro)"
    RESULTS+=("FAIL  $distro$note")
    "${INCUS[@]}" info "$name" --show-log >> "$log" 2>&1 || :
    if $KEEP_FAILED; then
      echo ">> Kept $name for inspection: incus exec $name -- bash" | tee -a "$log"
    else
      "${INCUS[@]}" delete --force "$name" >/dev/null 2>&1 || :
    fi
  fi
  ACTIVE=""
done
