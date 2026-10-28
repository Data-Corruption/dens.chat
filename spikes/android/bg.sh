#!/usr/bin/env bash
# The screen-off trials on a phone over wireless debugging: bg.html, a call
# inside one page, in the spike's page shown over the lock screen, with
# the screen off for 110 s per trial. Its tone plays through the screen
# going off and stops 15 s later, so the call is quiet for the minute
# Chromium waits before it freezes a hidden page. The page's lines each
# second and the spike's own log land in out/android-spike/SERIAL/bg-log.txt,
# the spike's audio players as Android sees them in bg-players.txt.
#
# Usage: spikes/android/bg.sh SERIAL [TRIAL...]
#   trials: free (not held), held, held-after (held, the contexts made
#   after the microphone); all three by default

set -euo pipefail
cd "$(dirname "$0")/../.."
CONTAINER=${CONTAINER:-dens-android-spike}
APP=chat.dens.spike
SERIAL=${1:?usage: bg.sh SERIAL [TRIAL...]}
shift
TRIALS=("$@")
[[ ${#TRIALS[@]} -gt 0 ]] || TRIALS=(free held held-after)
OUT=out/android-spike/$SERIAL
mkdir -p "$OUT"
: >"$OUT/bg-players.txt"
: >"$OUT/bg-marks.txt"

adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$SERIAL" "$@"; }
mark() { echo "$(date +%T.%3N) $*" | tee -a "$OUT/bg-marks.txt"; }
uid=$(adb shell pm list packages -U "$APP" | sed -n 's/.*uid:\([0-9]*\).*/\1/p')

# The page comes from the container, through the phone's localhost, the
# only origin the spike gives the microphone.
incus file push -q -p spikes/android/bg.html "$CONTAINER/root/spike/serve/bg.html"
# Each pkill runs alone: in a shell's command line beside the server's
# own command, its pattern would match that shell.
incus exec "$CONTAINER" -- pkill -f "[h]ttp.server --bind 127.0.0.1 18499" || true
incus exec "$CONTAINER" -- sh -c 'cd /root/spike/serve && setsid python3 -m http.server --bind 127.0.0.1 18499 >/dev/null 2>&1 </dev/null &'
adb reverse tcp:18499 tcp:18499 >/dev/null
incus exec "$CONTAINER" -- pkill -f "[l]ogcat -T 1 --uid" || true
incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$SERIAL" logcat -T 1 --uid="$uid" -v time >"$OUT/bg-log.txt" 2>&1 &
logcat=$!
cleanup() {
  kill "$logcat" 2>/dev/null || true
  incus exec "$CONTAINER" -- pkill -f "[l]ogcat -T 1 --uid" || true
  incus exec "$CONTAINER" -- pkill -f "[h]ttp.server --bind 127.0.0.1 18499" || true
  adb reverse --remove tcp:18499 >/dev/null 2>&1 || true
}
trap cleanup EXIT

players() {
  { echo "== $(date +%T) $1"; adb shell "dumpsys audio | grep 'u/pid:$uid/' || true" | tr -d '\r'; } >>"$OUT/bg-players.txt"
}

waitfor() {
  for _ in $(seq "$2"); do
    grep -q "$1" "$OUT/bg-log.txt" && return 0
    sleep 1
  done
  return 1
}

trial() {
  local name=$1 hold=true order=before
  [[ $name == free ]] && hold=false
  [[ $name == held-after ]] && order=after
  mark "$name: the page opens, held $hold, contexts $order the microphone"
  adb shell "am start -n $APP/.ReportActivity --es page 'http://localhost:18499/bg.html?trial=$name&order=$order&tone=25' --ez hold $hold --ez lock true" >/dev/null
  if ! waitfor "bg $name ready" 40; then
    mark "$name: the page never said it was ready"
    return 1
  fi
  sleep 10
  players "$name, on the screen"
  mark "$name: screen off"
  adb shell input keyevent KEYCODE_SLEEP
  sleep 5
  players "$name, screen off 5 s"
  sleep 7
  players "$name, screen off 12 s"
  sleep 58
  players "$name, screen off 70 s"
  sleep 30
  players "$name, screen off 100 s"
  sleep 10
  mark "$name: screen on"
  adb shell input keyevent KEYCODE_WAKEUP
  sleep 15
  players "$name, back on the screen 15 s"
}

for t in "${TRIALS[@]}"; do trial "$t"; done

# Dens's page again, behind the lock screen, and the screen off.
mark "done: Dens's page again"
adb shell "am start -n $APP/.ReportActivity --es page http://localhost:18484/ --ez lock false" >/dev/null
sleep 3
adb shell input keyevent KEYCODE_SLEEP
