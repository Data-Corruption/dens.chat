#!/usr/bin/env bash
# The Relay spike on a phone (questions 8 and 9), through the Android
# container's adb, as scripts/test/android/phone.sh works one: the spike's
# build of the app (spikes/relay/android/build.sh, then phone.sh SERIAL
# install arm64) runs the probe beside Dens and plays its page.
#
# Usage: spikes/relay/run/phone.sh SERIAL COMMAND [ARGS]
#   relay "ARGS" [wake] [nowifi] [page=QUERY]
#                                  start the probe with ARGS, as -peer 10.0.0.43 -role b;
#                                  wake holds the processor awake too, nowifi leaves Wi-Fi alone,
#                                  and page= is the player page's query, as auto=1&name=phone&call=30
#   stop                           stop the probe and its page
#   pull NAME                      copy the probe's log to relay/logs/phone/NAME.log
#   audio                          the app's players as Android has them, and the phone's battery
#   off SECONDS NAME               screen off for SECONDS, the players and the battery noted every
#                                  minute into relay/logs/phone/NAME-off.txt, then on again
#   tail                           the probe's last lines
#
# ANDROID_APP names the app, chat.dens.Dens.dev unless it says otherwise.

set -euo pipefail
cd "$(dirname "$0")/../.."
CONTAINER=${CONTAINER:-dens-android}
APP=${ANDROID_APP:-chat.dens.Dens.dev}
SERIAL=${1:?usage: phone.sh SERIAL COMMAND}
COMMAND=${2:?usage: phone.sh SERIAL COMMAND}
shift 2
LOGS=relay/logs/phone
mkdir -p "$LOGS"

adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$SERIAL" "$@"; }
uid() { adb shell pm list packages -U "$APP" | sed -n 's/.*uid:\([0-9]*\).*/\1/p' | tr -d '\r'; }

# audio prints each of the app's players, with its use and where it plays,
# the call's device if a call is on, and the battery.
audio() {
  local u
  u=$(uid)
  echo "== $(date +%T) $*"
  adb shell dumpsys audio | tr -d '\r' | grep -E "u/pid:$u/|uid:$u -- |Active communication device|mode owner|^ *mMode" | sed 's/^ *//' | cut -c1-260 || :
  adb shell dumpsys battery | tr -d '\r' | grep -E '^ *(level|Charge counter|AC powered|USB powered|Wireless powered|status):' | paste -sd' ' | sed 's/  */ /g'
}

case $COMMAND in
  relay)
    args=${1:?usage: phone.sh SERIAL relay "ARGS" [wake] [nowifi]}
    shift
    extras=()
    for opt in "$@"; do
      [[ $opt == wake ]] && extras+=(--ez wake true)
      [[ $opt == nowifi ]] && extras+=(--ez wifi false)
      [[ $opt == page=* ]] && extras+=(--es page "'${opt#page=}'")
    done
    adb shell "run-as $APP sh -c 'rm -f files/shell/relay.log files/shell/relay.out'" || :
    adb shell am start -n "$APP/chat.dens.shell.PageActivity" --es relay "'$args'" "${extras[@]}" >/dev/null
    sleep 4
    adb shell "run-as $APP sh -c 'tail -n 6 files/shell/relay.log; cat files/shell/relay.out'" | cut -c1-200
    ;;
  stop)
    adb shell am start -n "$APP/chat.dens.shell.PageActivity" --es relay stop >/dev/null
    ;;
  pull)
    name=${1:?usage: phone.sh SERIAL pull NAME}
    adb exec-out run-as "$APP" cat files/shell/relay.log >"$LOGS/$name.log"
    wc -l "$LOGS/$name.log"
    ;;
  tail)
    adb shell "run-as $APP sh -c 'tail -n ${1:-12} files/shell/relay.log'" | cut -c1-260
    ;;
  audio)
    audio now
    ;;
  off)
    secs=${1:?usage: phone.sh SERIAL off SECONDS NAME}
    name=${2:?usage: phone.sh SERIAL off SECONDS NAME}
    out=$LOGS/$name-off.txt
    : >"$out"
    woke=false
    trap '$woke || adb shell input keyevent KEYCODE_WAKEUP || :' EXIT
    audio "on screen" | tee -a "$out"
    adb shell input keyevent KEYCODE_SLEEP
    for ((t = 60; t <= secs; t += 60)); do
      sleep 60
      audio "off screen ${t}s" >>"$out"
      tail -n 1 "$out"
    done
    sleep $((secs % 60))
    adb shell input keyevent KEYCODE_WAKEUP
    woke=true
    audio "on screen again" | tee -a "$out"
    ;;
  *)
    sed -n '7,19p' "$0" >&2
    exit 2
    ;;
esac
