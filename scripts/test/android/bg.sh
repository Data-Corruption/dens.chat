#!/usr/bin/env bash
# A call off screen on a device (M12): the app's page in a call its member
# joined, with the screen off for SECONDS, ten minutes unless given, as the
# manual test runs it on a phone over wireless debugging. The page's side
# of the call every 5 seconds (call.mjs watch), and every 30 the app's
# audio players and recording as Android lists them, with where the call's
# sound plays, land in out/android/SERIAL/bg-*.txt, beside the app's log
# lines. It turns the screen on again at the end, and says whether the
# page ever hid or froze, what went each way off screen, and how Android
# played the call.
#
# Usage: scripts/test/android/bg.sh SERIAL [SECONDS]
#
# ANDROID_APP names the app, chat.dens.Dens.dev unless it says otherwise.

set -euo pipefail
cd "$(dirname "$0")/../../.."
CONTAINER=${CONTAINER:-dens-android}
APP=${ANDROID_APP:-chat.dens.Dens.dev}
SERIAL=${1:?usage: bg.sh SERIAL [SECONDS]}
SECONDS_OFF=${2:-600}
OUT=out/android/$SERIAL
mkdir -p "$OUT"

adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$SERIAL" "$@"; }
mark() { echo "$(date +%T) $*" | tee -a "$OUT/bg-marks.txt"; }
uid=$(adb shell pm list packages -U "$APP" | sed -n 's/.*uid:\([0-9]*\).*/\1/p' | tr -d '\r')
[[ -n $uid ]] || { echo "$APP isn't on $SERIAL" >&2; exit 1; }
: >"$OUT/bg-marks.txt"
: >"$OUT/bg-players.txt"

incus file push -q scripts/test/android/drive.mjs scripts/test/android/call.mjs "$CONTAINER/root/"
incus exec "$CONTAINER" --env ANDROID_APP="$APP" -- /root/node /root/call.mjs watch $((SECONDS_OFF + 30)) "$SERIAL" >"$OUT/bg-call.txt" 2>&1 &
watcher=$!
adb logcat -T 1 --uid="$uid" -v time >"$OUT/bg-log.txt" 2>&1 &
logcat=$!
woke=false
# The container's adb goes on reading the log after its incus exec stops:
# pkill finds it there, its pattern bracketed so it misses itself.
cleanup() {
  $woke || adb shell input keyevent KEYCODE_WAKEUP || :
  kill "$watcher" "$logcat" 2>/dev/null || :
  incus exec "$CONTAINER" -- pkill -f "[l]ogcat -T 1 --uid=$uid" || :
}
trap cleanup EXIT

# players notes the app's audio as Android has it: each player's use and
# device, whether the app records, and the call's communication device.
players() {
  {
    echo "== $(date +%T) $1"
    adb shell "dumpsys audio" | tr -d '\r' | grep -E "u/pid:$uid/|uid:$uid -- |Active communication device" | sed 's/^ *//' || :
  } >>"$OUT/bg-players.txt"
}

sleep 6
grep -q '"state":"connected"' "$OUT/bg-call.txt" || { echo "the app's page isn't in a call: join one first" >&2; exit 1; }
players "on screen"
mark "screen off for $SECONDS_OFF s"
adb shell input keyevent KEYCODE_SLEEP
off=$(date +%s%3N)
for ((i = 30; i <= SECONDS_OFF; i += 30)); do
  sleep 30
  players "off screen $i s"
done
sleep $((SECONDS_OFF % 30))
adb shell input keyevent KEYCODE_WAKEUP
woke=true
on=$(date +%s%3N)
mark "screen on"
players "on screen again"
wait "$watcher" || :

lines=$(sed -n '/^{"side":"watch","t"/p' "$OUT/bg-call.txt" | awk -v off="$off" -v on="$on" '{ match($0, /"at":[0-9]+/); at = substr($0, RSTART + 5, RLENGTH - 5) + 0; if (at >= off + 0 && at <= on + 0) print }')
echo
echo "Off screen, $(grep -c . <<<"$lines") samples of the page:"
echo "  shown: $(grep -c '"visible":"visible"' <<<"$lines"), hidden: $(grep -c '"visible":"hidden"' <<<"$lines"), frozen at last: $(tail -n 1 <<<"$lines" | grep -o '"frozen":[0-9]*' | cut -d: -f2)"
echo "  first: $(head -n 1 <<<"$lines" | grep -oE '"(rx|tx|speaking|state)":[^,]*' | tr '\n' ' ')"
echo "  last:  $(tail -n 1 <<<"$lines" | grep -oE '"(rx|tx|speaking|state)":[^,]*' | tr '\n' ' ')"
echo "The app's audio while off screen, in samples:"
sed -n '/off screen/,/on screen again/p' "$OUT/bg-players.txt" |
  sed -nE -e 's/.*deviceIds:\[([0-9]*)\].*state:([a-z]+).*usage=([A-Z_]+).*/  a player on device \1, \2, \3/p' \
    -e 's/.*source client=([A-Z_]+).*/  a recording from \1/p' \
    -e 's/.*Active communication device: .*type:([a-z_]+).*/  the call on the \1/p' | sort | uniq -c || :
echo "The call's routes, as the shell logged them:"
grep -E 'audio: ' "$OUT/bg-log.txt" | sed 's/^/  /' || echo "  none"
echo "Files in $OUT: bg-call.txt, bg-players.txt, bg-log.txt, bg-marks.txt"
