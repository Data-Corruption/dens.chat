#!/usr/bin/env bash
# Works a device through the Android container's adb (M12): the emulator,
# or a phone paired over wireless debugging. adb has the shell's rights on
# a phone, so everything here keeps to the app's own: its files (through
# run-as, which a development build allows), its log lines (by its UID),
# and the screen only while the app is in front.
#
# Usage: scripts/test/android/phone.sh SERIAL COMMAND [ARGS]
#   install ARCH         install out/android-ARCH.apk (arm64 or amd64) over the last
#   start                open the app, as its icon does
#   stop                 force-stop the app, and Dens with it
#   trust                give the app's Dens the emulator's den's root (SSL_CERT_FILE)
#   join NAME            join the emulator's den as NAME, from the page
#   pull                 copy the shell's files and Dens's logs to out/android/SERIAL
#   log                  the app's own log lines
#   shot FILE            a screenshot, if the app is in front
#   drive ARGS           drive.mjs on the page
#   serve [ARCH]         serve the APK to the device's browser at http://127.0.0.1:18499/dens.apk
#   pair CODE            pair with a phone's wireless debugging: SERIAL is the pairing
#                        address its screen shows, CODE the six digits
#   connect              connect to a paired phone at SERIAL, its debugging address
#
# ANDROID_APP names the app, chat.dens.Dens.dev unless it says otherwise.

set -euo pipefail
cd "$(dirname "$0")/../../.."
ROOT=$PWD
CONTAINER=${CONTAINER:-dens-android}
APP=${ANDROID_APP:-chat.dens.Dens.dev}
SERIAL=${1:?usage: phone.sh SERIAL COMMAND}
shift
COMMAND=${1:?usage: phone.sh SERIAL COMMAND}
shift
OUT=$ROOT/out/android/$SERIAL

adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$SERIAL" "$@"; }
# app runs a shell command as the app, in its data directory.
app() { adb shell run-as "$APP" sh -c "'$*'"; }
uid() { adb shell pm list packages -U "$APP" | sed -n 's/.*uid:\([0-9]*\).*/\1/p' | tr -d '\r'; }

case $COMMAND in
  install)
    incus file push -q "$ROOT/out/android-$1.apk" "$CONTAINER/root/android-$1.apk"
    adb install --no-incremental -r "/root/android-$1.apk"
    ;;
  start)
    adb shell am start -n "$APP/chat.dens.shell.PageActivity" >/dev/null
    ;;
  stop)
    adb shell am force-stop "$APP"
    ;;
  trust)
    scripts/test/android/den.sh root | adb shell "run-as $APP sh -c 'mkdir -p files/shell && cat > files/shell/den-root.crt'"
    echo "SSL_CERT_FILE=/data/user/0/$APP/files/shell/den-root.crt" | adb shell "run-as $APP sh -c 'cat > files/shell/env'"
    echo "the app's Dens trusts the den's root from its next start"
    ;;
  join)
    invite=$(scripts/test/android/den.sh invite)
    body=$(printf '{"invite":"%s","username":"%s","display_name":"%s","password":"%s den password"}' "$invite" "$1" "${1^}" "$1")
    incus file push -q scripts/test/android/drive.mjs "$CONTAINER/root/drive.mjs"
    # The answer holds the account's recovery codes and seal: only its status shows.
    incus exec "$CONTAINER" --env ANDROID_APP="$APP" -- /root/node /root/drive.mjs -s "$SERIAL" api POST /api/dens/join "$body" | cut -c1-3
    ;;
  pull)
    mkdir -p "$OUT"
    adb exec-out run-as "$APP" tar -C files -cf - shell dens/main/data/logs | tar -C "$OUT" -xf -
    find "$OUT" -type f | sort
    ;;
  log)
    adb logcat -d --uid="$(uid)" "$@"
    ;;
  shot)
    if ! adb shell dumpsys activity activities | grep -E 'mResumedActivity|topResumedActivity' | grep -q "$APP"; then
      echo "the app isn't in front; no screenshot" >&2
      exit 1
    fi
    adb exec-out screencap -p >"$1"
    ;;
  drive)
    incus file push -q scripts/test/android/drive.mjs "$CONTAINER/root/drive.mjs"
    incus exec "$CONTAINER" --env ANDROID_APP="$APP" -- /root/node /root/drive.mjs -s "$SERIAL" "$@"
    ;;
  serve)
    incus exec "$CONTAINER" -- mkdir -p /root/serve
    incus file push -q "$ROOT/out/android-${1:-arm64}.apk" "$CONTAINER/root/serve/dens.apk"
    adb reverse tcp:18499 tcp:18499
    incus exec "$CONTAINER" -- sh -c 'cd /root/serve && exec python3 -m http.server --bind 127.0.0.1 18499'
    ;;
  pair)
    incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb pair "$SERIAL" "$1"
    ;;
  connect)
    incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb connect "$SERIAL"
    ;;
  *)
    sed -n '8,23p' "$0" >&2
    exit 2
    ;;
esac
