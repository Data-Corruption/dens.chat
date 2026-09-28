#!/usr/bin/env bash

# Den e2e on Linux: a second machine joins a den through Caddy and stays
# connected across a den restart.
#
# One Incus container hosts the den behind the pinned Caddy, serving
# https://den.test with Caddy's internal certificate authority. Another,
# on a different distro, trusts that authority, installs Dens and joins
# with an invite from the owner. The den's service then restarts, and the
# member must reconnect. Both install from unsigned fixture releases
# through install.sh, like the lifecycle harness; the guest steps are in
# scripts/test/den-guest.sh.
#
# Usage:
#   ./scripts/test-den-e2e.sh
#   ./scripts/test-den-e2e.sh --den debian-13 --client fedora-44
#   KEEP_FAILED=true ./scripts/test-den-e2e.sh   # keep containers after a failure
#
# Logs go to out/den-e2e-logs/<run>/.

set -euo pipefail
# incus init reads instance config from a non-terminal stdin.
exec </dev/null

cd "$(dirname "$0")/.."

DEN_DISTRO=debian-13
CLIENT_DISTRO=fedora-44
while [[ $# -gt 0 ]]; do
  case "$1" in
    --den) DEN_DISTRO=$2; shift 2 ;;
    --client) CLIENT_DISTRO=$2; shift 2 ;;
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
for distro in "$DEN_DISTRO" "$CLIENT_DISTRO"; do
  distro_spec "$distro" >/dev/null || { echo "error: unknown distro '$distro'" >&2; exit 1; }
done
incus_setup

RUN_LOG_DIR="$PWD/out/den-e2e-logs/$(date -u +%Y%m%dT%H%M%SZ)-$$"
mkdir -p "$RUN_LOG_DIR"
HARNESS_DIR=$(mktemp -d)
DEN="dens-den-e2e-den-$$"
CLIENT="dens-den-e2e-client-$$"
passed=false

finish() {
  local status=$?
  trap - EXIT
  if ! $passed && $KEEP_FAILED; then
    echo ">> Kept $DEN and $CLIENT for inspection: incus exec <name> -- bash"
  else
    "${INCUS[@]}" delete --force "$DEN" "$CLIENT" >/dev/null 2>&1 || :
  fi
  rm -rf "$HARNESS_DIR"
  if $passed; then
    printf '\nDen e2e (%s den, %s client): PASSED\nLogs: %s\n' "$DEN_DISTRO" "$CLIENT_DISTRO" "$RUN_LOG_DIR"
  else
    printf '\nDen e2e (%s den, %s client): FAILED\nLogs: %s\n' "$DEN_DISTRO" "$CLIENT_DISTRO" "$RUN_LOG_DIR"
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

guest() {
  local name=$1
  shift
  timeout 600 "${INCUS[@]}" exec "$name" -- sh /root/den-guest.sh "$@"
}

run() {
  echo ">> Building the fixture release and fetching Caddy ..."
  scripts/test/fixture-releases.sh "$HARNESS_DIR/release" "$target"
  local caddy
  caddy=$(./scripts/vendor.sh caddy | sed -n 's/^caddy=//p')

  local name distro
  for name in "$DEN" "$CLIENT"; do
    distro=$CLIENT_DISTRO
    [[ "$name" == "$DEN" ]] && distro=$DEN_DISTRO
    launch_container "$distro" "$name"
    push_release "$name" "$HARNESS_DIR/release"
    "${INCUS[@]}" file push -q scripts/test/den-guest.sh "$name/root/den-guest.sh"
    guest "$name" prepare
  done

  echo ">> Hosting a den behind Caddy on $DEN_DISTRO"
  "${INCUS[@]}" file push -q --mode 0755 "$caddy" "$DEN/usr/local/bin/caddy"
  guest "$DEN" install --den
  guest "$DEN" pair
  guest "$DEN" caddy 8485
  local den_ip
  den_ip=$("${INCUS[@]}" list "$DEN" --format csv -c 4 | awk '{print $1}')
  [[ -n "$den_ip" ]] || { echo "error: the den container has no IPv4 address" >&2; return 1; }
  guest "$DEN" hosts 127.0.0.1 den.test

  echo ">> Trusting the den's certificate authority on $CLIENT_DISTRO"
  "${INCUS[@]}" file pull -q "$DEN/root/.local/share/caddy/pki/authorities/local/root.crt" - > "$HARNESS_DIR/root.crt"
  "${INCUS[@]}" file push -q "$HARNESS_DIR/root.crt" "$CLIENT/root/den-ca.crt"
  guest "$CLIENT" trust /root/den-ca.crt
  guest "$CLIENT" hosts "$den_ip" den.test
  guest "$CLIENT" install
  guest "$CLIENT" pair

  echo ">> Creating the den and inviting a member"
  local den_id invite role since
  den_id=$(guest "$DEN" create-den https://den.test)
  [[ -n "$den_id" ]] || { echo "error: creating the den returned no ID" >&2; return 1; }
  guest "$DEN" wait-connected 0 >/dev/null
  invite=$(guest "$DEN" invite "$den_id")

  echo ">> Joining from $CLIENT_DISTRO through Caddy"
  role=$(guest "$CLIENT" join "$invite")
  [[ "$role" == member ]] || { echo "error: joined as $role" >&2; return 1; }
  since=$(guest "$CLIENT" wait-connected 0)

  echo ">> Restarting the den's service"
  "${INCUS[@]}" exec "$DEN" -- systemctl restart dens@main
  since=$(guest "$CLIENT" wait-connected "$since")
  echo ">> The member reconnected after the restart"
  guest "$CLIENT" status
  echo
  "${INCUS[@]}" exec "$DEN" -- journalctl -u dens@main -b --no-pager -o cat | tail -n 20
}

run 2>&1 | tee "$RUN_LOG_DIR/den-e2e.log"
[[ "${PIPESTATUS[0]}" -eq 0 ]] && passed=true
