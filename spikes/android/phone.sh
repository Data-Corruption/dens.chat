#!/usr/bin/env bash
# Works a device through the spike container's adb: the emulator, or a
# phone paired over wireless debugging. adb has the shell's rights on a
# phone, so everything here keeps to the spike's own app: its files
# (through run-as, which the debuggable spike allows), its log lines (by
# its UID), and the screen only while the spike is in front.
#
# Usage: spikes/android/phone.sh SERIAL COMMAND [ARGS]
#   install ABI          install out/android-spike/dens-spike-ABI.apk over the last
#   trust                give the spike's Dens the emulator's den's root (SSL_CERT_FILE)
#   join NAME            join the emulator's den as NAME, from the page
#   do BUTTON [SECONDS]  press one of the spike's buttons, then print what it shows
#   pull                 copy the spike's reports and logs to out/android-spike/SERIAL
#   log                  the spike's own log lines
#   shot FILE            a screenshot, if the spike is in front
#   drive ARGS           drive.mjs on the WebView
#   serve                serve out/android-spike to the device on its 127.0.0.1:18499
#   pair CODE            pair with a phone's wireless debugging: SERIAL is the pairing
#                        address its screen shows, CODE the six digits
#   connect              connect to a paired phone at SERIAL, its debugging address

set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$PWD
OUT=$ROOT/out/android-spike
CONTAINER=${CONTAINER:-dens-android-spike}
APP=chat.dens.spike
SERIAL=${1:?usage: phone.sh SERIAL COMMAND}
shift
COMMAND=${1:?usage: phone.sh SERIAL COMMAND}
shift

adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$SERIAL" "$@"; }
# app runs a shell command as the spike, in its data directory.
app() { adb shell run-as "$APP" sh -c "'$*'"; }

case $COMMAND in
  install)
    incus file push -q "$OUT/dens-spike-$1.apk" "$CONTAINER/root/spike/out/dens-spike-$1.apk"
    adb install --no-incremental -r "/root/spike/out/dens-spike-$1.apk"
    ;;
  trust)
    spikes/android/den.sh root | adb shell "run-as $APP sh -c 'cat > files/spike/den-root.crt'"
    echo "SSL_CERT_FILE=/data/user/0/$APP/files/spike/den-root.crt" | adb shell "run-as $APP sh -c 'cat > files/spike/env'"
    echo "the spike's Dens trusts the den's root once its service starts again"
    ;;
  join)
    invite=$(spikes/android/den.sh invite)
    body=$(printf '{"invite":"%s","username":"%s","display_name":"%s","password":"%s den password"}' "$invite" "$1" "${1^}" "$1")
    incus file push -q spikes/android/drive.mjs "$CONTAINER/root/drive.mjs"
    # The answer holds the account's recovery codes and seal: only its status shows.
    incus exec "$CONTAINER" -- /root/node /root/drive.mjs -s "$SERIAL" api POST /api/dens/join "$body" | cut -c1-3
    ;;
  do)
    app rm -f files/spike/last.txt
    adb shell am start -n "$APP/.ReportActivity" --es "do" "$1" >/dev/null
    # A button that takes a while shows "…" until it's done.
    shown=""
    for _ in $(seq "${2:-30}"); do
      shown=$(app cat files/spike/last.txt 2>/dev/null || true)
      [[ -n $shown && $shown != *… ]] && break
      sleep 1
    done
    printf '%s\n' "${shown:-(nothing shown)}"
    ;;
  pull)
    mkdir -p "$OUT/$SERIAL"
    adb exec-out run-as "$APP" tar -C files/spike -cf - --exclude=config . | tar -C "$OUT/$SERIAL" -xf -
    ls -l "$OUT/$SERIAL"
    ;;
  log)
    uid=$(adb shell pm list packages -U "$APP" | sed -n 's/.*uid:\([0-9]*\).*/\1/p')
    adb logcat -d --uid="$uid" "$@"
    ;;
  shot)
    if ! adb shell dumpsys activity activities | grep -E 'mResumedActivity|topResumedActivity' | grep -q "$APP"; then
      echo "the spike isn't in front; no screenshot" >&2
      exit 1
    fi
    adb exec-out screencap -p >"$1"
    ;;
  drive)
    incus file push -q spikes/android/drive.mjs "$CONTAINER/root/drive.mjs"
    incus exec "$CONTAINER" -- /root/node /root/drive.mjs -s "$SERIAL" "$@"
    ;;
  serve)
    incus exec "$CONTAINER" -- mkdir -p /root/spike/serve
    incus file push -q "$OUT/dens-spike-${1:-arm64-v8a}.apk" "$CONTAINER/root/spike/serve/dens-spike.apk"
    adb reverse tcp:18499 tcp:18499
    incus exec "$CONTAINER" -- sh -c 'cd /root/spike/serve && exec python3 -m http.server --bind 127.0.0.1 18499'
    ;;
  pair)
    incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb pair "$SERIAL" "$1"
    ;;
  connect)
    incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb connect "$SERIAL"
    ;;
  *)
    sed -n '8,20p' "$0" >&2
    exit 2
    ;;
esac
