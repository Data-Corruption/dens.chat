#!/usr/bin/env bash

# Den e2e on Linux: a second machine joins a den through Caddy, chats, and
# stays connected across a den restart; then a ban shuts it out.
#
# One Incus container hosts the den behind the pinned Caddy, serving
# https://den.test with Caddy's internal certificate authority. Another,
# on a different distro, trusts that authority, installs Dens and joins
# with an invite from the owner. The two chat, the den's service restarts,
# and the member must reconnect with the whole history. The owner then
# sends a DM and bans the member, whose connection must close at once and
# who can't join again under the same name. Both install from unsigned
# fixture releases through install.sh, like the lifecycle harness; the
# guest steps are in scripts/test/den-guest.sh.
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

  echo ">> Chatting"
  local channel history
  channel=$(guest "$DEN" channel "$den_id" general)
  [[ "$(guest "$CLIENT" wait-channel "$den_id")" == "$channel" ]] || { echo "error: the member sees another channel" >&2; return 1; }
  guest "$CLIENT" send "$den_id" "$channel" "hello from the member" >/dev/null
  guest "$DEN" send "$den_id" "$channel" "hello back, @bob" >/dev/null
  history=$(guest "$DEN" history "$den_id" "$channel")
  [[ "$history" == $'hello from the member\nhello back, @bob' ]] || { echo "error: the owner's history is: $history" >&2; return 1; }

  echo ">> Restarting the den's service"
  "${INCUS[@]}" exec "$DEN" -- systemctl restart dens@main
  since=$(guest "$CLIENT" wait-connected "$since")
  echo ">> The member reconnected after the restart"
  guest "$DEN" wait-connected 0 >/dev/null
  guest "$DEN" send "$den_id" "$channel" "after the restart" >/dev/null
  history=$(guest "$CLIENT" history "$den_id" "$channel")
  [[ "$history" == $'hello from the member\nhello back, @bob\nafter the restart' ]] ||
    { echo "error: the member's history after the restart is: $history" >&2; return 1; }
  echo ">> The member's history is complete after the restart"

  echo ">> A direct message"
  local bob dm
  bob=$(guest "$DEN" member-id "$den_id" bob)
  dm=$(guest "$DEN" dm "$den_id" "$bob")
  guest "$DEN" send "$den_id" "$dm" "a private word" >/dev/null
  [[ "$(guest "$CLIENT" wait-dm "$den_id")" == "$dm" ]] || { echo "error: the DM didn't reach the member" >&2; return 1; }
  history=$(guest "$CLIENT" history "$den_id" "$dm")
  [[ "$history" == "a private word" ]] || { echo "error: the member's DM reads: $history" >&2; return 1; }

  echo ">> A phone photo with GPS data, through Caddy"
  local upload file stripped size preview kind stored
  "${INCUS[@]}" file push -q scripts/test/gps-photo.jpg "$CLIENT/root/gps-photo.jpg"
  upload=$(guest "$CLIENT" upload "$den_id" /root/gps-photo.jpg IMG_0001.jpg)
  read -r file stripped size preview <<<"$upload"
  # Orientation 6 turns the 400x300 photo a quarter: it shows 300x400.
  [[ "$stripped $size $preview" == "true 300x400 preview" ]] || { echo "error: the upload came back as: $upload" >&2; return 1; }
  guest "$CLIENT" send-file "$den_id" "$channel" "$file" >/dev/null
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/photo.jpg)
  [[ "$kind" == image/jpeg ]] || { echo "error: the photo came as $kind" >&2; return 1; }
  [[ "$(guest "$DEN" jpeg /root/photo.jpg)" == "400x300 JFIF,orientation 6" ]] ||
    { echo "error: the photo arrived as: $(guest "$DEN" jpeg /root/photo.jpg)" >&2; return 1; }
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/preview.jpg thumb)
  [[ "$kind" == image/jpeg && "$(guest "$DEN" jpeg /root/preview.jpg)" == "300x400 nothing" ]] ||
    { echo "error: the preview is $kind $(guest "$DEN" jpeg /root/preview.jpg)" >&2; return 1; }
  stored=$(guest "$DEN" sealed)
  echo ">> The photo arrived stripped, upright and with a preview; the den holds ${stored} sealed files"

  echo ">> Banning the member"
  local started took reason refusal
  guest "$CLIENT" status
  echo
  started=$(date +%s%3N)
  guest "$DEN" ban "$den_id" "$bob"
  reason=$(guest "$CLIENT" wait-removed)
  took=$(( $(date +%s%3N) - started ))
  [[ "$reason" == "You were banned from this den." ]] || { echo "error: the member was told: $reason" >&2; return 1; }
  # The limit covers two container execs; the den closes the socket itself
  # within milliseconds.
  (( took < 5000 )) || { echo "error: the ban took ${took} ms to reach the member" >&2; return 1; }
  echo ">> The ban closed the member's connection within ${took} ms"
  invite=$(guest "$DEN" invite "$den_id")
  refusal=$(guest "$CLIENT" join-refused "$invite")
  [[ "$refusal" == *banned* ]] || { echo "error: rejoining after the ban: $refusal" >&2; return 1; }
  echo ">> The banned member can't come back as bob"
  echo
  "${INCUS[@]}" exec "$DEN" -- journalctl -u dens@main -b --no-pager -o cat | tail -n 20
}

run 2>&1 | tee "$RUN_LOG_DIR/den-e2e.log"
[[ "${PIPESTATUS[0]}" -eq 0 ]] && passed=true
