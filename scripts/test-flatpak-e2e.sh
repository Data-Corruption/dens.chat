#!/usr/bin/env bash

# Flatpak e2e (M9.3): Dens's Flatpak, made from this tree's binary into a
# repository signed as the release's is, installed from it for a member and
# run in a Debian 13 container with no desktop: the member's session bus is
# her systemd user manager's, and Flatpak's portal checks for updates every
# few seconds, which the app may install by itself. Two launches at once
# start one service; the page's API pairs and sets the local password, and
# a restarted service starts locked, until the password opens it; Remember,
# with no keyring there to take it, keeps the key in a file, and the next
# start is unlocked. The Flatpak joins a den behind Caddy in a second
# container, as the den e2e's is hosted, trusting its certificate
# authority. Flatpak's update monitor then finds the fixture's next version
# in the repository, which the page installs, restarting into it, which
# migrates after a snapshot of the data; the den reconnects. Rolled back to
# the build before, Dens refuses the newer data and names the snapshot,
# whose restore the rolled-back build takes, starting locked.
#
# The Flatpak's container starts from a local image, dens-e2e/flatpak, with
# Flatpak, the member and the freedesktop runtime installed, which the
# first run makes, downloading the runtime from Flathub; FRESH_IMAGE=true
# makes it again.
#
# Usage:
#   ./scripts/test-flatpak-e2e.sh
#   KEEP_FAILED=true ./scripts/test-flatpak-e2e.sh   # keep containers after a failure
#
# Logs go to out/flatpak-e2e-logs/<run>/.

set -euo pipefail
# incus init reads instance config from a non-terminal stdin.
exec </dev/null

cd "$(dirname "$0")/.."

case "${KEEP_FAILED:-false}" in
  true) KEEP_FAILED=true ;;
  false|"") KEEP_FAILED=false ;;
  *) echo "error: KEEP_FAILED must be true or false" >&2; exit 1 ;;
esac
[[ $# -eq 0 ]] || { echo "usage: $0" >&2; exit 2; }

# shellcheck source=test/incus.sh
source scripts/test/incus.sh
incus_setup

IMAGE=dens-e2e/flatpak
RUN_LOG_DIR="$PWD/out/flatpak-e2e-logs/$(date -u +%Y%m%dT%H%M%SZ)-$$"
mkdir -p "$RUN_LOG_DIR"
HARNESS_DIR=$(mktemp -d)
DEN="dens-flatpak-e2e-den-$$"
FLATPAK="dens-flatpak-e2e-$$"
passed=false

finish() {
  local status=$?
  trap - EXIT
  "${INCUS[@]}" exec "$FLATPAK" -- sh -c 'cat /home/alice/.var/app/chat.dens.Dens/data/dens/main/data/logs/*.log' \
    >"$RUN_LOG_DIR/flatpak-service.log" 2>/dev/null || :
  if ! $passed && $KEEP_FAILED; then
    echo ">> Kept $DEN and $FLATPAK for inspection: incus exec <name> -- bash"
  else
    "${INCUS[@]}" delete --force "$DEN" "$FLATPAK" >/dev/null 2>&1 || :
  fi
  rm -rf "$HARNESS_DIR"
  if $passed; then
    printf '\nFlatpak e2e: PASSED\nLogs: %s\n' "$RUN_LOG_DIR"
  else
    printf '\nFlatpak e2e: FAILED\nLogs: %s\n' "$RUN_LOG_DIR"
    [[ "$status" -ne 0 ]] || status=1
  fi
  exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

case "$(uname -m)" in
  x86_64|amd64) target=linux-amd64 ;;
  aarch64|arm64) target=linux-arm64 ;;
  *) echo "error: unsupported host architecture $(uname -m)" >&2; exit 1 ;;
esac

den() { timeout 600 "${INCUS[@]}" exec "$DEN" -- sh /root/den-guest.sh "$@"; }
flatpak() { timeout 900 "${INCUS[@]}" exec "$FLATPAK" -- sh /root/flatpak-guest.sh "$@"; }

# push_scripts NAME copies what the Flatpak's container runs: its steps,
# and the script and files that make the commits.
push_scripts() {
  "${INCUS[@]}" exec "$1" -- mkdir -p /root/scripts
  "${INCUS[@]}" file push -q scripts/test/flatpak-guest.sh "$1/root/flatpak-guest.sh"
  "${INCUS[@]}" file push -q scripts/flatpak.sh "$1/root/scripts/flatpak.sh"
  "${INCUS[@]}" file push -q -r scripts/flatpak "$1/root/scripts/"
}

# image makes the Flatpak's container image, once: Debian 13 with Flatpak,
# the member and the runtime, which is downloaded from Flathub.
image() {
  if [[ "${FRESH_IMAGE:-false}" == true ]]; then
    "${INCUS[@]}" image delete "$IMAGE" >/dev/null 2>&1 || :
  fi
  "${INCUS[@]}" image info "$IMAGE" >/dev/null 2>&1 && return
  local maker="dens-flatpak-e2e-image-$$"
  echo ">> Making $IMAGE: Flatpak and the freedesktop runtime (a download from Flathub, once)"
  launch_container debian-13 "$maker"
  push_scripts "$maker"
  timeout 1800 "${INCUS[@]}" exec "$maker" -- sh /root/flatpak-guest.sh image
  "${INCUS[@]}" stop "$maker"
  "${INCUS[@]}" publish "$maker" --alias "$IMAGE" >/dev/null
  "${INCUS[@]}" delete --force "$maker"
}

run() {
  echo ">> Building the fixture releases and fetching Caddy ..."
  scripts/test/fixture-releases.sh "$HARNESS_DIR/release" "$target"
  local caddy first next
  caddy=$(./scripts/vendor.sh caddy | sed -n 's/^caddy=//p')
  first=v99.0.1-e2e
  next=v99.0.2-e2e
  gzip -dc "$HARNESS_DIR/release/releases/$first/$target.gz" >"$HARNESS_DIR/dens-$first"
  gzip -dc "$HARNESS_DIR/release/next/releases/$next/$target.gz" >"$HARNESS_DIR/dens-$next"

  image
  echo ">> Hosting a den behind Caddy, as the den e2e does"
  launch_container debian-13 "$DEN"
  push_release "$DEN" "$HARNESS_DIR/release"
  "${INCUS[@]}" file push -q scripts/test/den-guest.sh "$DEN/root/den-guest.sh"
  "${INCUS[@]}" file push -q --mode 0755 "$caddy" "$DEN/usr/local/bin/caddy"
  den prepare
  den install
  den pair
  den hosting-on
  den caddy 8485
  den hosts 127.0.0.1 den.test
  local den_id invite den_ip
  den_id=$(den create-den https://den.test)
  [[ -n "$den_id" ]] || { echo "error: creating the den returned no ID" >&2; return 1; }
  invite=$(den invite "$den_id")
  den_ip=$("${INCUS[@]}" list "$DEN" --format csv -c 4 | awk '{print $1}')
  [[ -n "$den_ip" ]] || { echo "error: the den container has no IPv4 address" >&2; return 1; }
  "${INCUS[@]}" file pull -q "$DEN/root/.local/share/caddy/pki/authorities/local/root.crt" - >"$HARNESS_DIR/den-ca.crt"

  echo ">> Installing the Flatpak of $first from its repository"
  echo ">> Launching $FLATPAK from $IMAGE"
  "${INCUS[@]}" init "$IMAGE" "$FLATPAK" -c security.nesting=true >/dev/null
  "${INCUS[@]}" start "$FLATPAK"
  wait_for_boot "$FLATPAK"
  push_scripts "$FLATPAK"
  local version
  for version in "$first" "$next"; do
    "${INCUS[@]}" file push -q --mode 0755 "$HARNESS_DIR/dens-$version" "$FLATPAK/root/dens-$version"
  done
  "${INCUS[@]}" file push -q "$HARNESS_DIR/den-ca.crt" "$FLATPAK/root/den-ca.crt"
  flatpak prepare
  flatpak commit "$first"
  flatpak install

  echo ">> Two launches at once open the page, from one service"
  flatpak launch-twice
  flatpak pair "$(flatpak link)"
  [[ "$(flatpak status | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["version"], d["locked"])')" == "$first False" ]] ||
    { echo "error: the new instance's status: $(flatpak status)" >&2; return 1; }

  echo ">> A restarted service starts locked, and the local password opens it"
  flatpak quit
  flatpak launch >"$RUN_LOG_DIR/launch-locked.out"
  flatpak unlock

  echo ">> Remember, with no keyring to take the key, keeps it in a file, and Dens starts unlocked"
  flatpak remember
  flatpak quit
  flatpak launch >"$RUN_LOG_DIR/launch-remembered.out"
  [[ "$(flatpak status | python3 -c 'import json,sys; print(json.load(sys.stdin)["locked"])')" == False ]] ||
    { echo "error: a remembered key started locked" >&2; return 1; }

  echo ">> Joining the den through Caddy, from the sandbox"
  flatpak trust /root/den-ca.crt
  flatpak hosts "$den_ip" den.test
  flatpak quit
  flatpak launch >"$RUN_LOG_DIR/launch-trusting.out"
  [[ "$(flatpak join "$invite")" == member ]] || { echo "error: the Flatpak didn't join as a member" >&2; return 1; }
  flatpak wait-connected

  echo ">> Flatpak's update monitor finds $next, which the page installs, restarting into it"
  flatpak commit "$next"
  flatpak wait-update
  flatpak update "$next"
  flatpak wait-connected

  echo ">> Rolled back to $first, Dens refuses the newer data and names the snapshot"
  flatpak rollback "$first"
  flatpak restore
  flatpak launch >"$RUN_LOG_DIR/launch-restored.out"
  # A restore pairs no browser, as on every install.
  flatpak pair "$(flatpak link)"
  flatpak unlock
  [[ "$(flatpak status | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])')" == "$first" ]] ||
    { echo "error: the restored instance runs $(flatpak status)" >&2; return 1; }
  flatpak wait-connected
  passed=true
}

run
