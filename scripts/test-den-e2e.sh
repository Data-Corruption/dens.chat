#!/usr/bin/env bash

# Den e2e on Linux: a second machine joins a den through Caddy, chats,
# stays connected across a den restart, and calls the owner through the
# den's media ports, over UDP and over TCP alone; the two start a private
# DM; the member recovers their account on a fresh install and signs the
# old one out; then a ban shuts them out.
#
# One Incus container hosts the den behind the pinned Caddy, serving
# https://den.test with Caddy's internal certificate authority. Another, on
# a different distro, trusts that authority, installs Dens and joins with an
# invite from the owner. The two chat, the den's service restarts, and the
# member must reconnect with the whole history. The two then call each other
# in a voice channel, a test binary with Pion standing in for each one's
# browser, and each must hear the other: over UDP, and with the member's
# side over TCP alone. The member sends a phone photo that must arrive
# stripped, a phone video that must arrive stripped with its preview and
# play from any byte, and an iPhone HEIC that must arrive as a JPEG. The
# owner opens a DM, which takes no message until both members type each
# other's check digits, and then carries text, a photo and a video the den
# stores only sealed. The owner shares a checklist with the member, and the
# two tick different boxes at the same moment, all of which must stay. A
# second instance on the member's machine stands in for a fresh one: it
# signs in by the den's address with a recovery code, whose new password
# must sign the first install out at once, and reads the DM only once the
# member types their DM seal. The new password alone doesn't sign the first
# install in again: the fresh one approves it, the two comparing digits, and
# hands it the seal, with which it reads the DM. Signed out once more from
# the fresh one's device list, it signs in again the same way, with the
# digits typed into the fresh one first, which must go on showing its own.
# The owner then bans the member, whose connections must close at once and
# who can't join again under the same name. Both install from unsigned
# fixture releases through install.sh, like the lifecycle harness; the guest
# steps are in scripts/test/den-guest.sh.
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

# member runs a step on the member's first install.
member() {
  guest "$CLIENT" "$@"
}

# fresh runs a step as instance fresh on the member's machine: the member's
# new install, with its own keys and none of the first one's data.
fresh() {
  timeout 600 "${INCUS[@]}" exec "$CLIENT" --env INSTANCE=fresh --env PORT=18484 -- sh /root/den-guest.sh "$@"
}

# approve_sign_in NEW OLD DEN_ID PASSWORD [old-first]: sign one of the
# member's installs in with the password, approved from another, each a
# function that runs a step there. Each shows digits, which the harness
# types into the other, as the member would: first into the old install
# with old-first, which must still show its own digits once it approved.
approve_sign_in() {
  local new=$1 old=$2 den_id=$3 password=$4 order=${5:-} id request new_half old_half shown
  id=$($new sign-in https://den.test bob "$password")
  [[ -n "$id" ]] || { echo "error: the password alone signed the device in" >&2; return 1; }
  request=$($old request "$den_id")
  old_half=$($old answer "$den_id" "$request")
  new_half=$($new sign-in-half "$id")
  [[ "$new_half" != "$old_half" ]] || { echo "error: both devices show the same digits" >&2; return 1; }
  if [[ "$order" == old-first ]]; then
    $old approve "$den_id" "$request" "$new_half"
    shown=$($old approved-half "$den_id" "$request")
    [[ "$shown" == "$old_half" ]] || { echo "error: once it approved, the old install shows $shown" >&2; return 1; }
    $new check-sign-in "$id" "$shown"
  else
    $new check-sign-in "$id" "$old_half"
    $old approve "$den_id" "$request" "$new_half"
  fi
  $new wait-signed-in "$id"
  $old dismiss "$den_id" "$request"
}

run() {
  echo ">> Building the fixture release and the voice probe, and fetching Caddy ..."
  scripts/test/fixture-releases.sh "$HARNESS_DIR/release" "$target"
  # The voice probe is a test binary: Pion standing in for each side's
  # browser in a call.
  GOOS=linux GOARCH=${target#linux-} CGO_ENABLED=0 go test -c -o "$HARNESS_DIR/voice-probe" ./internal/platform/http/client
  local caddy
  caddy=$(./scripts/vendor.sh caddy | sed -n 's/^caddy=//p')

  local name distro
  for name in "$DEN" "$CLIENT"; do
    distro=$CLIENT_DISTRO
    [[ "$name" == "$DEN" ]] && distro=$DEN_DISTRO
    launch_container "$distro" "$name"
    push_release "$name" "$HARNESS_DIR/release"
    "${INCUS[@]}" file push -q scripts/test/den-guest.sh "$name/root/den-guest.sh"
    "${INCUS[@]}" file push -q --mode 0755 "$HARNESS_DIR/voice-probe" "$name/root/voice-probe"
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

  echo ">> A call through the den's media ports"
  local lounge alice_id bob_id network
  lounge=$(guest "$DEN" channel "$den_id" Lounge voice)
  [[ "$(guest "$CLIENT" named-channel "$den_id" Lounge)" == "$lounge" ]] || { echo "error: the member sees another voice channel" >&2; return 1; }
  alice_id=$(guest "$DEN" member-id "$den_id" alice)
  bob_id=$(guest "$DEN" member-id "$den_id" bob)
  # The owner's install reaches its den on this machine's own addresses;
  # the member's, at the address den.test resolves to. Over TCP alone, as
  # on a network that blocks UDP, the member's side takes the fallback port.
  for network in udp tcp; do
    guest "$DEN" voice-probe "$den_id" "$lounge" "$bob_id" udp >"$RUN_LOG_DIR/call-owner-$network.log" 2>&1 &
    local owner=$!
    if ! guest "$CLIENT" voice-probe "$den_id" "$lounge" "$alice_id" "$network" >"$RUN_LOG_DIR/call-member-$network.log" 2>&1; then
      wait "$owner" || :
      cat "$RUN_LOG_DIR/call-member-$network.log" "$RUN_LOG_DIR/call-owner-$network.log" >&2
      return 1
    fi
    wait "$owner" || { cat "$RUN_LOG_DIR/call-owner-$network.log" >&2; return 1; }
    echo ">> The owner and the member heard each other, the member over $network"
  done


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

  echo ">> A phone video with GPS data, and an iPhone HEIC, through Caddy"
  local media type name duration converted
  for media in with-gps.mov rotated.heic; do
    "${INCUS[@]}" file push -q "internal/media/ffmpeg/testdata/$media" "$CLIENT/root/$media"
  done
  read -r file type size preview converted name duration <<<"$(guest "$CLIENT" upload-media "$den_id" /root/with-gps.mov IMG_0003.MOV)"
  # 568x320, turned a quarter, for four seconds.
  [[ "$type $size $preview $converted $name" == "video/mp4 320x568 preview as-is IMG_0003.MOV" && "$duration" -gt 3900 ]] ||
    { echo "error: the video came back as: $file $type $size $preview $converted $name $duration" >&2; return 1; }
  guest "$CLIENT" send-file "$den_id" "$channel" "$file" >/dev/null
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/video.mp4)
  [[ "$kind" == video/mp4 ]] || { echo "error: the video came as $kind" >&2; return 1; }
  guest "$DEN" clean /root/video.mp4
  guest "$DEN" range "$den_id" "$file" /root/video.mp4 200000 299999
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/video-preview.jpg thumb)
  [[ "$kind" == image/jpeg && "$(guest "$DEN" jpeg /root/video-preview.jpg)" == "320x568 nothing" ]] ||
    { echo "error: the video's preview is $kind $(guest "$DEN" jpeg /root/video-preview.jpg)" >&2; return 1; }
  read -r file type size preview converted name duration <<<"$(guest "$CLIENT" upload-media "$den_id" /root/rotated.heic IMG_0004.HEIC)"
  [[ "$type $size $preview $converted $name" == "image/jpeg 3024x4032 preview converted IMG_0004.jpg" ]] ||
    { echo "error: the HEIC came back as: $file $type $size $preview $converted $name" >&2; return 1; }
  guest "$CLIENT" send-file "$den_id" "$channel" "$file" >/dev/null
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/heic.jpg)
  [[ "$kind" == image/jpeg && "$(guest "$DEN" jpeg /root/heic.jpg)" == "3024x4032 ICC_PROFILE" ]] ||
    { echo "error: the HEIC arrived as $kind $(guest "$DEN" jpeg /root/heic.jpg)" >&2; return 1; }
  guest "$DEN" clean /root/heic.jpg
  echo ">> The video arrived stripped with its preview and plays from any byte; the HEIC arrived as an upright JPEG in its own colors"

  echo ">> A private DM, once both members compare check codes"
  local bob dm owner_half member_half refusal
  bob=$(guest "$DEN" member-id "$den_id" bob)
  dm=$(guest "$DEN" dm "$den_id" "$bob")
  [[ "$(member wait-dm "$den_id")" == "$dm" ]] || { echo "error: the DM didn't reach the member" >&2; return 1; }
  refusal=$(guest "$DEN" send-refused "$den_id" "$dm" "too soon")
  [[ "$refusal" == *"compare check codes"* ]] || { echo "error: sending before the check: $refusal" >&2; return 1; }
  guest "$DEN" start-key "$den_id" "$dm"
  owner_half=$(guest "$DEN" dm-half "$den_id" "$dm")
  member_half=$(member dm-half "$den_id" "$dm")
  [[ "$owner_half" != "$member_half" ]] || { echo "error: both members see the same digits" >&2; return 1; }
  refusal=$(guest "$DEN" check-dm-refused "$den_id" "$dm" "0000 0000 0000 0000")
  [[ "$refusal" == *"don't match"* ]] || { echo "error: the wrong digits: $refusal" >&2; return 1; }
  guest "$DEN" check-dm "$den_id" "$dm" "$member_half"
  member check-dm "$den_id" "$dm" "$owner_half"
  guest "$DEN" send "$den_id" "$dm" "a private word" >/dev/null
  history=$(member history "$den_id" "$dm")
  [[ "$history" == "a private word" ]] || { echo "error: the member's DM reads: $history" >&2; return 1; }
  upload=$(member upload "$den_id" /root/gps-photo.jpg IMG_0002.jpg "$dm")
  read -r file stripped size preview <<<"$upload"
  [[ "$stripped $size $preview" == "true 300x400 preview" ]] || { echo "error: the DM photo came back as: $upload" >&2; return 1; }
  member send-file "$den_id" "$dm" "$file" >/dev/null
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/dm-photo.jpg)
  [[ "$kind" == image/jpeg && "$(guest "$DEN" jpeg /root/dm-photo.jpg)" == "400x300 JFIF,orientation 6" ]] ||
    { echo "error: the DM photo arrived as $kind $(guest "$DEN" jpeg /root/dm-photo.jpg)" >&2; return 1; }
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/dm-preview.jpg thumb)
  [[ "$kind" == image/jpeg ]] || { echo "error: the DM photo's preview came as $kind" >&2; return 1; }
  read -r file type size preview converted name duration <<<"$(member upload-media "$den_id" /root/with-gps.mov IMG_0005.MOV "$dm")"
  [[ "$type $size $preview" == "video/mp4 320x568 preview" && "$duration" -gt 3900 ]] ||
    { echo "error: the DM video came back as: $file $type $size $preview $duration" >&2; return 1; }
  member send-file "$den_id" "$dm" "$file" >/dev/null
  # The owner's service learns the video's key from the message, which the
  # page reads before it asks for the file.
  guest "$DEN" history "$den_id" "$dm" >/dev/null
  kind=$(guest "$DEN" fetch "$den_id" "$file" /root/dm-video.mp4)
  [[ "$kind" == video/mp4 ]] || { echo "error: the DM video came as $kind" >&2; return 1; }
  guest "$DEN" clean /root/dm-video.mp4
  guest "$DEN" range "$den_id" "$file" /root/dm-video.mp4 200000 299999
  guest "$DEN" dm-sealed "a private word"
  stored=$(guest "$DEN" sealed)
  echo ">> The DM took messages only after both typed each other's digits; its text, photo and video reached the other side, and the den holds ${stored} sealed files"

  echo ">> Two members tick one checklist at the same moment"
  local list owner_ticks member_ticks text
  list=$(guest "$DEN" send-shared "$den_id" "$channel" "$bob" '[ ] item 0\n[ ] item 1\n[ ] item 2\n[ ] item 3\n[ ] item 4\n[ ] item 5')
  guest "$DEN" ticks "$den_id" "$list" 0 2 4 &
  owner_ticks=$!
  guest "$CLIENT" ticks "$den_id" "$list" 1 3 5 &
  member_ticks=$!
  wait "$owner_ticks" || { echo "error: the owner's ticks failed" >&2; return 1; }
  wait "$member_ticks" || { echo "error: the member's ticks failed" >&2; return 1; }
  text=$(guest "$CLIENT" text-of "$den_id" "$channel" "$list")
  [[ "$text" == "[x] item 0|[x] item 1|[x] item 2|[x] item 3|[x] item 4|[x] item 5" ]] ||
    { echo "error: the checklist reads: $text" >&2; return 1; }
  echo ">> Every tick from both members stayed"

  echo ">> Recovering the member's account on a fresh install"
  local code left signed_out fingerprint key started took reason refusal
  code=$(guest "$CLIENT" code 0)
  guest "$CLIENT" install --instance fresh --client-port 18484
  fresh pair
  started=$(date +%s%3N)
  # By address, as someone with no invite would, and in the wrong case.
  read -r left signed_out fingerprint <<<"$(fresh recover https://den.test Bob "$code" "new bob password")"
  [[ "$left $signed_out" == "9 1" ]] ||
    { echo "error: recovering left $left codes and signed out $signed_out devices" >&2; return 1; }
  [[ "$fingerprint" == "$(guest "$DEN" fingerprint)" ]] ||
    { echo "error: the fresh install found den $fingerprint, not the owner's" >&2; return 1; }
  reason=$(guest "$CLIENT" wait-out revoked)
  took=$(( $(date +%s%3N) - started ))
  [[ "$reason" == "Your den password was changed on another device"* ]] ||
    { echo "error: the first install was told: $reason" >&2; return 1; }
  # The limit covers container execs; the den closes the socket itself
  # within milliseconds.
  (( took < 5000 )) || { echo "error: the new password took ${took} ms to sign the first install out" >&2; return 1; }
  fresh wait-connected 0 >/dev/null
  echo ">> The fresh install signed in with a recovery code, shows the owner's den ID, and signed the first install out within ${took} ms"
  local seal
  [[ "$(fresh has-seal)" == false ]] || { echo "error: a recovery code brought a DM seal" >&2; return 1; }
  history=$(fresh history "$den_id" "$dm")
  [[ "$history" != *"a private word"* ]] || { echo "error: the fresh install read the DM without the seal" >&2; return 1; }
  seal=$(member seal)
  [[ -n "$seal" ]] || { echo "error: joining showed no new DM seal" >&2; return 1; }
  fresh type-seal "$den_id" "$seal"
  history=$(fresh history "$den_id" "$dm")
  [[ "$history" == "a private word"* ]] || { echo "error: with the seal typed, the DM reads: $history" >&2; return 1; }
  echo ">> The fresh install reads the DM once the member typed their seal"
  refusal=$(member sign-in-refused https://den.test bob "bob password")
  [[ "$refusal" == *"don't match"* ]] || { echo "error: signing in with the old password: $refusal" >&2; return 1; }
  approve_sign_in member fresh "$den_id" "new bob password"
  member wait-connected 0 >/dev/null
  history=$(member history "$den_id" "$dm")
  [[ "$history" == "a private word"* ]] || { echo "error: the approved install's DM reads: $history" >&2; return 1; }
  echo ">> The old password is gone; the new one signs the first install in again once the fresh one approves it, and hands it the seal"

  echo ">> Signing the first install out from the fresh one"
  key=$(fresh other-device "$den_id")
  started=$(date +%s%3N)
  fresh revoke "$den_id" "$key"
  reason=$(guest "$CLIENT" wait-out revoked)
  took=$(( $(date +%s%3N) - started ))
  [[ "$reason" == "This device was signed out of this den. Sign in again with your den password." ]] ||
    { echo "error: the first install was told: $reason" >&2; return 1; }
  (( took < 5000 )) || { echo "error: signing out took ${took} ms to reach the first install" >&2; return 1; }
  approve_sign_in member fresh "$den_id" "new bob password" old-first
  member wait-connected 0 >/dev/null
  echo ">> The first install was signed out within ${took} ms, and signed in again with the fresh one's approval, typed there first"

  echo ">> Banning the member"
  guest "$CLIENT" status
  echo
  started=$(date +%s%3N)
  guest "$DEN" ban "$den_id" "$bob"
  reason=$(guest "$CLIENT" wait-out removed)
  took=$(( $(date +%s%3N) - started ))
  [[ "$reason" == "You were banned from this den." ]] || { echo "error: the member was told: $reason" >&2; return 1; }
  (( took < 5000 )) || { echo "error: the ban took ${took} ms to reach the member" >&2; return 1; }
  reason=$(fresh wait-out removed)
  [[ "$reason" == "You were banned from this den." ]] || { echo "error: the fresh install was told: $reason" >&2; return 1; }
  echo ">> The ban closed the member's connections within ${took} ms"
  invite=$(guest "$DEN" invite "$den_id")
  refusal=$(guest "$CLIENT" join-refused "$invite")
  [[ "$refusal" == *banned* ]] || { echo "error: rejoining after the ban: $refusal" >&2; return 1; }
  echo ">> The banned member can't come back as bob"
  echo
  "${INCUS[@]}" exec "$DEN" -- journalctl -u dens@main -b --no-pager -o cat | tail -n 20
}

run 2>&1 | tee "$RUN_LOG_DIR/den-e2e.log"
[[ "${PIPESTATUS[0]}" -eq 0 ]] && passed=true
