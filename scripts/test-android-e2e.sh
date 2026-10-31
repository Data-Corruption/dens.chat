#!/usr/bin/env bash

# Android e2e (M12.1): the Android app's development APK, built from this
# tree, in Android's emulator, joining a den behind Caddy in the browser
# checks' container, as a member's phone does. In Android 17's emulator,
# its screen 390 CSS pixels wide (M13), the app installs, makes its data
# key in the Keystore, starts Dens and pairs its page without a token; the
# page takes a local password and joins the den through its forms, typed
# into by Android's keyboard (M13.3), with a picture and an audit of each
# screen, and the den lists the phone by the name the emulator has, with
# its model under it; a message goes each way from the composer; a phone
# photo goes as a
# smaller copy, and saved from the viewer it reaches Downloads with no GPS
# or camera left in it. A call with the den's owner goes on through two
# and a half minutes off screen (M12.2), most of them silent, as the app
# holds its page as shown, and lets the page and the microphone go once
# the call ends. A DM from the den's owner while the app is behind the
# others posts its notification (M12.3), whose tap opens the page at the
# message, in place on the page that shows (M13.1), and reading it takes
# the next back. Dens killed under the shell
# starts again, and the
# app force-stopped and opened again keeps its page's session; Back leaves
# the page, and a link opens outside it. At 390 CSS pixels (M13.1), by
# Android's own taps and swipes, the member gets around the phone layout's
# panes, from Home to a channel, the list and the members, and Back walks
# up them to Home, closing the settings on the way, and out of the app.
# By touch (M13.2), a long press opens a message's actions in a sheet,
# which a swipe down closes, and Back too; the keyboard comes up, shown
# beside the emulator's own, leaving the composer and the newest message
# in sight, and three lines go by the send button; and a tip opens on a
# tap and closes on the next. Then every member's screen is walked for its
# picture and the audit of its layout, which fails on a control under 24
# pixels, a list's row under 44 or a control only a hover shows
# (scripts/test/android/walk.mjs). The settings and Manage open as lists
# of pages, which Back walks out of, and the phone's settings hold no
# keyboard shortcuts or screen share; the viewer, on a message's two
# photos, moves to the second by a swipe, zooms by a double tap, and Back
# closes it; and a dialog, a sheet, closes by a swipe down (M13.3). Then
# the screens are walked again with Android's font scale at 1.3, as a
# member with larger text has it. The host package's tests, which
# hold Android's seams, run in the emulator as the app. Then the app's
# release, from the fixture releases (scripts/test/android/releases.sh),
# updates itself (M12.4): its updater refuses a newer release whose
# checksums another workflow signed, before downloading its APK, then
# checks the release signed as it should be, and the tap on its
# notification hands the APK to Android's installer, which updates the app
# to it. Android 12's
# emulator, whose WebView is older than the page needs, runs Dens and says
# to update the WebView, and the release's app updates itself there too.
#
# It runs in two persistent containers: dens-browsers, which
# scripts/test/browser/setup.sh makes and the den runs in, and
# dens-android, which scripts/test/android/setup.sh makes, with the
# emulator. Both stay; the emulators stop at the end.
#
# Usage:
#   ./scripts/test-android-e2e.sh              # build first: the den's binary, then the APKs
#   ./scripts/test-android-e2e.sh --no-build   # use the builds already in out/
#
# Logs go to out/android-e2e-logs/<run>/.

set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/.."

build=true
case ${1:-} in
  --no-build) build=false ;;
  "") ;;
  *) echo "usage: $0 [--no-build]" >&2; exit 2 ;;
esac
[[ "$(uname -m)" == x86_64 ]] || { echo "error: the Android e2e needs an x86_64 host" >&2; exit 1; }

CONTAINER=${CONTAINER:-dens-android}
APP=chat.dens.Dens.dev
# The release's app, from the fixture releases, which updates itself, and
# where the e2e serves their host: the emulator's loopback, which adb
# forwards to the container's (M12.4).
RELEASE_APP=chat.dens.Dens
RELEASES_PORT=18480
# The development build empties out/, so the run's logs wait for the
# builds in a directory of their own.
RUN_LOG_DIR=$(mktemp -d)
NEW=emulator-5554
OLD=emulator-5556
passed=false

adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb -s "$serial" "$@"; }
phone() { scripts/test/android/phone.sh "$serial" "$@"; }
check() { incus exec "$CONTAINER" -- /root/node /root/check.mjs "$serial" "$@" | tee -a "$RUN_LOG_DIR/checks-$serial.log"; }
den() { scripts/test/android/den.sh "$@"; }
shell_log() { adb shell run-as "$APP" cat files/shell/shell.log | tr -d '\r'; }
# front is the app's activity on top.
front() { [[ $(adb shell dumpsys activity activities) =~ topResumedActivity=[^$'\n']*$APP/ ]]; }
# tap TRIES REGEX taps the first thing on screen whose text matches REGEX,
# trying once a second, and keeps the screen's layout in the logs if it
# never comes.
tap() {
  local tries=$1 re=$2 bounds
  for _ in $(seq "$tries"); do
    bounds=$(adb exec-out uiautomator dump /dev/tty 2>/dev/null | grep -oE "text=\"($re)\"[^>]*bounds=\"\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]\"" |
      head -n 1 | grep -oE '\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]' | tr -c '0-9\n' ' ' || :)
    if [[ -n "$bounds" ]]; then
      read -r x1 y1 x2 y2 <<<"$bounds"
      adb shell input tap $(((x1 + x2) / 2)) $(((y1 + y2) / 2))
      return 0
    fi
    sleep 1
  done
  adb exec-out uiautomator dump /dev/tty >"$RUN_LOG_DIR/screen-$serial.xml" 2>/dev/null || :
  return 1
}
ok() { printf '✓ %s\n' "$*"; }
fail() {
  printf '✗ %s\n' "$*" >&2
  exit 1
}

# waitfor TRIES WHAT COMMAND... runs the command once a second until it
# succeeds.
waitfor() {
  local tries=$1 what=$2
  shift 2
  for _ in $(seq "$tries"); do
    "$@" >/dev/null 2>&1 && return 0
    sleep 1
  done
  fail "waited $tries s for $what"
}

# stop_releases_host stops the fixture releases' host, if it runs.
stop_releases_host() {
  # shellcheck disable=SC2016 # the container's shell expands it
  incus exec "$CONTAINER" -- sh -c 'test -f /root/releases.pid && kill "$(cat /root/releases.pid)" 2>/dev/null; rm -f /root/releases.pid' || :
}

finish() {
  local status=$?
  trap - EXIT
  for serial in $NEW $OLD; do
    # adb waits for a device that isn't there.
    incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb devices | grep -q "^${serial}[[:space:]]*device" || continue
    phone pull >/dev/null 2>&1 && cp -r "out/android/$serial" "$RUN_LOG_DIR/" 2>/dev/null || :
    adb logcat -d --uid="$(adb shell pm list packages -U "$APP" 2>/dev/null | sed -n 's/.*uid:\([0-9]*\).*/\1/p' | tr -d '\r')" \
      >"$RUN_LOG_DIR/logcat-$serial.txt" 2>/dev/null || :
    # The release's app, from the fixture releases, which run-as can't read.
    if adb shell pm list packages "$RELEASE_APP" 2>/dev/null | grep -q "package:$RELEASE_APP\b"; then
      ANDROID_APP=$RELEASE_APP phone log >"$RUN_LOG_DIR/logcat-release-$serial.txt" 2>/dev/null || :
      adb logcat -d -s PackageInstaller:* ActivityTaskManager:I >"$RUN_LOG_DIR/logcat-installer-$serial.txt" 2>/dev/null || :
    fi
  done
  stop_releases_host
  # The emulator keeps a density it was given across restarts, and its
  # keyboard's setting and font scale.
  serial=$NEW
  if incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb devices | grep -q "^${serial}[[:space:]]*device"; then
    adb shell wm density reset || :
    adb shell settings put secure show_ime_with_hard_keyboard 0 || :
    adb shell settings put system font_scale 1.0 || :
  fi
  incus exec "$CONTAINER" -- /root/box.sh halt 5554 || :
  incus exec "$CONTAINER" -- /root/box.sh halt 5556 || :
  if $passed; then
    printf '\nAndroid e2e: PASSED\nLogs: %s\n' "$RUN_LOG_DIR"
  else
    printf '\nAndroid e2e: FAILED\nLogs: %s\n' "$RUN_LOG_DIR"
    [[ "$status" -ne 0 ]] || status=1
  fi
  exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if $build; then
  echo ">> Building the den's binary, and putting it in dens-browsers"
  scripts/test/browser/setup.sh >"$RUN_LOG_DIR/browser-setup.log" 2>&1 || { tail -n 30 "$RUN_LOG_DIR/browser-setup.log"; fail "setting up dens-browsers"; }
  echo ">> Building the APKs"
  ./scripts/build.sh --android >"$RUN_LOG_DIR/build.log" 2>&1 || { tail -n 30 "$RUN_LOG_DIR/build.log"; fail "building the APKs"; }
  echo ">> Building the fixture releases"
  rm -rf out/android-releases
  scripts/test/android/releases.sh out/android-releases >"$RUN_LOG_DIR/releases.log" 2>&1 ||
    { tail -n 30 "$RUN_LOG_DIR/releases.log"; fail "building the fixture releases"; }
fi
built=$RUN_LOG_DIR
RUN_LOG_DIR="$PWD/out/android-e2e-logs/$(date -u +%Y%m%dT%H%M%SZ)-$$"
mkdir -p "$RUN_LOG_DIR"
mv "$built"/* "$RUN_LOG_DIR/" 2>/dev/null || :
rmdir "$built"
[[ -f out/android-amd64.apk ]] || fail "no out/android-amd64.apk: build it with ./scripts/build.sh --android"
[[ -f out/android-releases/version ]] || fail "no fixture releases: build them with scripts/test/android/releases.sh out/android-releases"
# The host package's tests, built as the app runs Dens: cgo through the
# NDK, for the emulator.
tools=$(./scripts/vendor.sh android 2>/dev/null | sed -n 's/^android=//p')
GOOS=android GOARCH=amd64 CGO_ENABLED=1 CC="$tools/ndk/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android31-clang" \
  go test -c -ldflags=-checklinkname=0 -o "$RUN_LOG_DIR/host.test" ./internal/platform/host

echo ">> Setting up $CONTAINER"
scripts/test/android/setup.sh >"$RUN_LOG_DIR/android-setup.log" 2>&1 || { tail -n 30 "$RUN_LOG_DIR/android-setup.log"; fail "setting up $CONTAINER"; }
echo ">> Starting the den"
den up | tee -a "$RUN_LOG_DIR/den.log"
general=$(den general)

incus file push -q scripts/test/browser/screens.mjs scripts/test/android/walk.mjs scripts/test/android/drive.mjs "$CONTAINER/root/"
walk() { incus exec "$CONTAINER" --env WALK_PREFIX="${WALK_PREFIX:-phone-}" -- /root/node /root/walk.mjs "$serial" /root/walk /root/walk-ctx.json "$@" | tee -a "$RUN_LOG_DIR/walk-$serial.log"; }
incus exec "$CONTAINER" -- rm -rf /root/walk

echo ">> Android 17"
serial=$NEW
incus exec "$CONTAINER" -- /root/box.sh boot a17 5554
adb uninstall "$APP" >/dev/null 2>&1 || :
# The phone layout (M13), at 390 CSS pixels, its 1080-pixel screen at 443
# dpi, from the first screen on; and a name of the phone's own, as a
# phone's owner gives it in Android's settings, which dens list it by
# beside its model (M13.3).
adb shell wm density 443
adb shell settings put system font_scale 1.0
adb shell "settings put global device_name 'Dens e2e phone'"
phone install amd64 >/dev/null
# The app's first start makes its key and pairs its page; trusting the
# den's authority needs the app's files, so Dens starts again with it.
phone start
waitfor 60 "the page" check paired
phone trust >/dev/null
adb shell input keyevent KEYCODE_HOME
sleep 2
phone stop
phone start
waitfor 60 "the page again" check paired
[[ $(shell_log | grep -c 'page: pairing the page') == 1 ]] || fail "the page paired more than once"
shell_log | grep -q 'keys: made the data key' || fail "no data key made"
ok "installed: the shell made the data key in the Keystore, started Dens, and paired the page without a token"

# The den keeps the members runs before made: each run's is new. The page
# takes the local password and joins through its forms, with the keyboard
# shown beside the emulator's own.
member=android_$(date +%s)
printf '{"invite":"%s","member":"%s","name":"%s","password":"%s den password","local":"phone local password","device":"Dens e2e phone","model":"%s"}\n' \
  "$(den invite)" "$member" "${member^}" "$member" "$(adb shell getprop ro.product.model | tr -d '\r')" | incus exec "$CONTAINER" -- sh -c 'cat > /root/walk-ctx.json'
adb shell settings put secure show_ime_with_hard_keyboard 1
walk first || fail "setting the local password and joining the den through the page's forms"
adb shell settings put secure show_ime_with_hard_keyboard 0
ok "the page took the local password and joined the den through its forms, by Android's taps and keyboard, and the den lists the phone by its name, with its model under it"
phone drive go "$(phone drive eval 'location.origin' | tr -d '"')/" >/dev/null
check general
check say "hello from the phone"
den say "$general" "hello from desk" >/dev/null
check heard "hello from desk"
ok "joined the den, and a message went each way"

incus file push -q "$(./scripts/vendor.sh browser-media 2>/dev/null | sed -n 's/^browser-media=//p')/pixel-2.jpg" "$CONTAINER/root/pixel-2.jpg"
incus exec "$CONTAINER" -- sh -c "/opt/android/sdk/platform-tools/adb -s $serial shell -T \"run-as $APP sh -c 'mkdir -p files/e2e && cat > files/e2e/pixel-2.jpg'\" </root/pixel-2.jpg"
check photo "/data/user/0/$APP/files/e2e/pixel-2.jpg"
# Runs before left their photos in Downloads, which outlive the app that
# saved them, so this run's is the name that's new, as MediaStore numbers
# it, once the shell has written it.
downloads() { adb shell ls /sdcard/Download/ | tr -d '\r' | grep '^pixel-2' || :; }
saved() { shell_log | grep -q 'download: saved'; }
before=$(downloads)
check download
waitfor 30 "the photo saved" saved
name=$(downloads | grep -vxF -e "$before" | head -n 1)
[[ -n "$name" ]] || fail "no new photo in Downloads"
adb pull "/sdcard/Download/$name" /root/saved.jpg >/dev/null 2>&1
incus file pull -q "$CONTAINER/root/saved.jpg" "$RUN_LOG_DIR/saved.jpg"
written=$(shell_log | sed -n 's/.*download: saved \([0-9]*\) bytes.*/\1/p' | tail -n 1)
[[ "$(stat -c %s "$RUN_LOG_DIR/saved.jpg")" == "$written" ]] || fail "Downloads holds $(stat -c %s "$RUN_LOG_DIR/saved.jpg") bytes of the $written the shell wrote"
exiftool=$(./scripts/vendor.sh exiftool 2>/dev/null | sed -n 's/^exiftool=//p')
left=$("$exiftool" -s -s -s -GPS:all -Make -Model -DateTimeOriginal "$RUN_LOG_DIR/saved.jpg")
[[ -z "$left" ]] || fail "the saved photo kept: $left"
ok "a phone photo went as its smaller copy, and saved from the viewer it reached Downloads, its GPS and camera gone"

# A call with desk, whose fake microphone speaks but for most of the two
# and a half minutes the phone's screen is off. The emulator's microphone
# hears silence, so the phone's page hears and sends nothing then: Chromium
# would freeze a page that quiet a minute after it hid, but the app tells
# its WebView it shows while the page has the microphone.
echo ">> A call, off screen"
incus file push -q --uid 1000 --gid 1000 scripts/test/android/call.mjs dens-browsers/home/tester/browser/call.mjs
incus exec dens-browsers --user 1000 --group 1000 --cwd /home/tester/browser --env HOME=/home/tester --env XDG_RUNTIME_DIR=/run/user/1000 -- \
  /home/tester/node call.mjs desk 215 quiet 20 150 >"$RUN_LOG_DIR/call-desk.log" 2>&1 &
desk_call=$!
sleep 5
incus exec "$CONTAINER" -- /root/node /root/call.mjs phone 200 "$serial" >"$RUN_LOG_DIR/call-phone.log" 2>&1 &
phone_call=$!
sleep 3
[[ $(phone allow) == "allowed 2" ]] || fail "Android didn't ask for the microphone and notifications"
granted() { shell_log | grep -q 'page: granted it the microphone'; }
waitfor 20 "the page to get the microphone" granted
routed() { shell_log | grep -q 'audio: the call plays on the loudspeaker as the call starts'; }
waitfor 20 "the call's route" routed
# Whole, since grep -q leaves adb writing to a closed pipe, which
# pipefail counts as failing.
services() { adb shell dumpsys activity services "$APP"; }
[[ $(services) == *'types=0x40000080'* ]] || fail "the service hasn't the microphone's type"
[[ $(adb shell dumpsys notification --noredact) == *'In a call, on the loudspeaker'* ]] || fail "no call in the service's notification"
ok "Android asked for the microphone and notifications, the service took the microphone's type, and the call went to the loudspeaker"
sleep 8
adb shell input keyevent KEYCODE_SLEEP
off=$(date +%s%3N)
sleep 150
adb shell input keyevent KEYCODE_WAKEUP
on=$(date +%s%3N)
adb shell wm dismiss-keyguard >/dev/null 2>&1 || :
wait "$phone_call" || fail "the phone's side of the call: $(tail -n 3 "$RUN_LOG_DIR/call-phone.log")"
wait "$desk_call" || fail "desk's side of the call: $(tail -n 3 "$RUN_LOG_DIR/call-desk.log")"
"$(./scripts/vendor.sh node 2>/dev/null | sed -n 's/^node=//p')" scripts/test/android/call.mjs judge \
  "$RUN_LOG_DIR/call-phone.log" "$RUN_LOG_DIR/call-desk.log" "$off" "$on" | tee "$RUN_LOG_DIR/call-judged.log" ||
  fail "the call off screen"
ended() { shell_log | grep -q 'service: the page stopped recording, so its call is over'; }
waitfor 25 "the call's end" ended
shell_log | grep -q 'page: no longer held' || fail "the page still held after its call"
[[ $(services) == *'types=0x40000000'* ]] || fail "the service kept the microphone's type after the call"
ok "the call held through $(((on - off) / 1000)) s off screen, both ways, and once it ended the service let the microphone and the page go"

# Notifications (M12.3). Desk and the phone's member check their DM's
# digits, as members comparing codes do, and the member turns DMs on in the
# page's settings. Android asked for notifications at the call, which the
# member allowed, so it doesn't ask again. A DM from desk while the app is
# behind the others posts through the shell, and its tap opens the page at
# the message; read on the den, the next is taken back.
echo ">> A DM's notification"
pagejson() { phone drive eval "fetch('$1').then((r) => r.json()).then((v) => $2)" | tr -d '"'; }
den_id=$(pagejson /api/dens 'v.dens[0].den_id')
member_id=$(pagejson "/api/dens/$den_id/state" 'v.me.id')
dm=$(den dm "$member_id")
desk_half="" phone_half=""
for _ in $(seq 60); do
  desk_half=$(den api GET "/api/dens/$den_id/dms/$dm/check" | python3 -c 'import json, sys; print(json.load(sys.stdin).get("half", ""))')
  phone_half=$(pagejson "/api/dens/$den_id/dms/$dm/check" "v.half || ''")
  [[ -n "$desk_half" && -n "$phone_half" ]] && break
  sleep 0.5
done
[[ -n "$desk_half" && -n "$phone_half" ]] || fail "the DM's check codes never came"
den api POST "/api/dens/$den_id/dms/$dm/check" "{\"digits\":\"$phone_half\"}" >/dev/null
phone drive api POST "/api/dens/$den_id/dms/$dm/check" "{\"digits\":\"$desk_half\"}" | grep -q '^200' || fail "the phone's check of the DM"
phone drive api POST /api/settings '{"notifications":{"dms":true}}' | grep -q '^200' || fail "turning DMs on"
adb shell input keyevent KEYCODE_HOME
sleep 2
notifications() { adb shell dumpsys notification --noredact | tr -d '\r'; }
shows() { [[ $(notifications) == *"$1"* ]]; }
first=$(den say "$dm" "a DM for the phone")
waitfor 20 "the DM's notification" shows "a DM for the phone"
[[ $(notifications) == *"pkg=$APP"*"channel=messages"* ]] || fail "the notification isn't the app's, in its Messages channel"
! shell_log | grep -q 'asking Android for POST_NOTIFICATIONS' || fail "Android was asked for notifications again"
# The page, which still shows behind the others, takes the tap in place
# (M13.1), as a call on it needs: what it holds now is there after.
phone drive eval 'window.heldOver = true' >/dev/null
adb shell cmd statusbar expand-notifications
sleep 2
bounds=$(adb exec-out uiautomator dump /dev/tty 2>/dev/null | grep -oE 'text="a DM for the phone"[^>]*bounds="\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]"' |
  head -n 1 | grep -oE '\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]' | tr -c '0-9\n' ' ' || :)
[[ -n "$bounds" ]] || fail "no notification to tap in the shade"
read -r x1 y1 x2 y2 <<<"$bounds"
adb shell input tap $(((x1 + x2) / 2)) $(((y1 + y2) / 2))
waitfor 20 "the page in front" front
check at "$den_id/$dm/$first" >/dev/null || fail "the tap didn't open the page at the DM's message"
shell_log | grep -q "page: opening a notification's message" || fail "the shell didn't open the message"
[[ $(phone drive eval 'window.heldOver === true') == true ]] || fail "the tap loaded the page again rather than opening the message on it"
ok "a DM posted its notification while the app was behind the others, and its tap opened the page at the message, in place"
adb shell input keyevent KEYCODE_HOME
sleep 2
second=$(den say "$dm" "another DM for the phone")
waitfor 20 "the second DM's notification" shows "another DM for the phone"
phone drive api PUT "/api/dens/$den_id/channels/$dm/read" "{\"message_id\":\"$second\"}" | grep -q '^200' || fail "reading the DM"
hidden() { ! shows "another DM for the phone"; }
waitfor 20 "the notification taken back" hidden
ok "read on the den, the DM's notification was taken back"

adb shell run-as "$APP" sh -c "'kill -9 \$(pidof libdens.so)'"
waitfor 30 "Dens started again" sh -c "scripts/test/android/phone.sh $serial log -s Dens:* | grep -q 'Dens ended (signal 9)'"
waitfor 30 "the page again" check paired
adb shell input keyevent KEYCODE_HOME
sleep 2
phone stop
phone start
waitfor 30 "the page after a force-stop" check paired
[[ $(shell_log | grep -c 'page: pairing the page') == 1 ]] || fail "the page paired again after a force-stop"
ok "Dens killed started again, and the app force-stopped kept its page's session"

phone drive eval 'location.href = "https://example.com/", true' >/dev/null
waitfor 10 "the link to go out" sh -c "scripts/test/android/phone.sh $serial log -s Dens:* | grep -q 'a https link went out'"
[[ $(phone drive eval 'location.pathname') == '"/"' ]] || fail "the page left for the link"
# The browser takes a while to come up the first time, and would come up
# over the page again: each move waits for the last.
behind() { ! front; }
waitfor 20 "the link's app in front" behind
adb shell am start -n "$APP/chat.dens.shell.PageActivity" >/dev/null 2>&1
waitfor 10 "the page in front again" front
adb shell input keyevent KEYCODE_BACK
waitfor 5 "Back to leave the page at its first address" behind
ok "a link opened outside the page, and Back left it"

# The phone layout (M13), in a page opened afresh, whose history begins at
# Home. Desk sends a message of two photos, for the viewer's swipe.
echo ">> Getting around, at 390 pixels"
random=$(den channel random)
den api PATCH "/api/dens/$den_id/channels/$general" '{"description":"Anything goes here: say hi, and **be kind**.\nA second line, which shows once the description is opened."}' >/dev/null
photos="\"$(den upload photos/pixel-2.jpg one.jpg)\",\"$(den upload photos/iphone-5.jpg two.jpg)\""
den say "$general" "two photos from desk" "$photos" >/dev/null
phone start
waitfor 20 "the page in front" front
waitfor 30 "the page" check paired
printf '{"den":"%s","general":"%s","random":"%s","dm":"%s"}\n' "$den_id" "$general" "$random" "$dm" |
  incus exec "$CONTAINER" -- sh -c 'cat > /root/walk-ctx.json'
walk around || fail "getting around the phone layout"
ok "from Home, by Android's own taps and swipes, the member got to a channel, the list and the members and back, and Back closed the settings, went up to Home, and left the app"
# The emulator has a keyboard of its own, which keeps Android's from
# showing unless told to.
adb shell settings put secure show_ime_with_hard_keyboard 1
phone start
waitfor 20 "the page in front" front
walk touch || fail "touch at 390 pixels"
adb shell settings put secure show_ime_with_hard_keyboard 0
ok "by Android's own touch, a long press opened a message's actions in a sheet, which a swipe down and Back closed; with the keyboard up, the composer and the newest message stayed in sight, and three lines went by the send button; and a tip opened on a tap"
phone start
waitfor 20 "the page in front" front
walk pages || fail "dialogs, the settings and the viewer at 390 pixels"
ok "the settings and Manage opened as lists of pages, which Back walked out of, and the phone's settings held no keyboard shortcuts or screen share; the viewer moved to a message's second photo by a swipe, zoomed by a double tap, and Back closed it; and a dialog, a sheet, closed by a swipe down"
phone start
waitfor 20 "the page in front" front
walk screens || fail "the screens at 390 pixels"
ok "every member's screen at 390 pixels fits a finger, its picture and its audit in the logs"
# Larger text (M13.3): Android's font scale at 1.3, which the WebView's
# text follows, as a member with larger text has it. A new font scale
# makes the activity, and its page, anew.
adb shell settings put system font_scale 1.3
phone start
waitfor 20 "the page in front" front
waitfor 30 "the page" check paired
WALK_PREFIX=large- walk screens || fail "the screens at 390 pixels, with larger text"
adb shell settings put system font_scale 1.0
mkdir -p "$RUN_LOG_DIR/walk-$serial"
incus exec "$CONTAINER" -- tar -C /root/walk -cf - . | tar -C "$RUN_LOG_DIR/walk-$serial" -xf -
adb shell wm density reset
ok "with Android's font scale at 1.3, every member's screen at 390 pixels still fits, its picture and its audit in the logs"

incus file push -q "$RUN_LOG_DIR/host.test" "$CONTAINER/root/host.test"
adb push /root/host.test /data/local/tmp/host.test >/dev/null 2>&1
adb shell "run-as $APP sh -c 'mkdir -p files/e2e/t && cp /data/local/tmp/host.test files/e2e/ && cd files/e2e && TMPDIR=\$PWD/t ./host.test -test.v'" \
  >"$RUN_LOG_DIR/host-tests.log" 2>&1 || { grep -E '^(--- FAIL|FAIL)' "$RUN_LOG_DIR/host-tests.log" >&2; fail "the host package's tests, as the app"; }
ok "the host package's tests passed as the app: $(grep -c '^--- PASS' "$RUN_LOG_DIR/host-tests.log") passed, $(grep -c '^--- SKIP' "$RUN_LOG_DIR/host-tests.log") skipped"

# update_from_fixtures has the release's app update itself from the fixture
# releases (M12.4), on the emulator at $serial:
# v99.0.1-e2e, installed as the browser would, finds v99.0.2-e2e on its
# release host. Served first with checksums another workflow signed, the
# updater refuses them before downloading the APK; served as signed, it
# downloads the APK and checks it, and the shell posts that it's ready.
# The tap hands it to Android's installer, which updates the app once the
# member confirms, and Android starts the app's service again.
update_from_fixtures() {
  echo ">> An update, from the fixture releases"
  release_log() { ANDROID_APP=$RELEASE_APP scripts/test/android/phone.sh "$serial" log -s Dens:* | tr -d '\r'; }
  version_of() { adb shell dumpsys package "$RELEASE_APP" | tr -d '\r' | sed -n 's/^ *versionName=//p' | head -n 1; }
  fetched() { incus exec "$CONTAINER" -- grep -c "GET /releases/v99.0.2-e2e/$1 " /root/releases.log || :; }
  host_dir=/root/android-releases/releases/v99.0.2-e2e
  incus exec "$CONTAINER" -- rm -rf /root/android-releases
  incus file push -q -r out/android-releases "$CONTAINER/root/"
  incus exec "$CONTAINER" -- sh -c "cp $host_dir/checksums.txt.cosign.bundle $host_dir/release.cosign.bundle && cp $host_dir/other.cosign.bundle $host_dir/checksums.txt.cosign.bundle"
  # The host keeps its PID, which stops it: its shell becomes it.
  incus exec "$CONTAINER" -- sh -c "echo \$\$ >/root/releases.pid && cd /root/android-releases && exec python3 -m http.server --bind 127.0.0.1 $RELEASES_PORT >/root/releases.log 2>&1" &
  sleep 1
  adb reverse "tcp:$RELEASES_PORT" "tcp:$RELEASES_PORT" >/dev/null
  adb uninstall "$RELEASE_APP" >/dev/null 2>&1 || :
  adb install --no-incremental /root/android-releases/releases/v99.0.1-e2e/android-amd64.apk >/dev/null
  [[ $(version_of) == v99.0.1-e2e ]] || fail "the release's app isn't v99.0.1-e2e"
  # What the member allowed by now: notifications, and installing apps from
  # Dens, which Android asks for at its first update.
  adb shell pm grant "$RELEASE_APP" android.permission.POST_NOTIFICATIONS 2>/dev/null || :
  adb shell appops set "$RELEASE_APP" REQUEST_INSTALL_PACKAGES allow
  adb shell am start -n "$RELEASE_APP/chat.dens.shell.PageActivity" >/dev/null
  bundle_fetched() { [[ $(fetched checksums.txt.cosign.bundle) -ge 1 ]]; }
  waitfor 90 "the release's checksums and their signature fetched" bundle_fetched
  sleep 5
  [[ $(fetched android-amd64.apk) == 0 ]] || fail "the updater downloaded an APK whose release another workflow signed"
  ! release_log | grep -q 'update: v99.0.2-e2e is ready' || fail "the shell offered a release another workflow signed"
  ok "the updater refused v99.0.2-e2e's checksums signed by another workflow, before downloading its APK"

  incus exec "$CONTAINER" -- cp "$host_dir/release.cosign.bundle" "$host_dir/checksums.txt.cosign.bundle"
  adb shell input keyevent KEYCODE_HOME
  adb shell am force-stop "$RELEASE_APP"
  adb shell am start -n "$RELEASE_APP/chat.dens.shell.PageActivity" >/dev/null
  ready() { release_log | grep -q 'update: v99.0.2-e2e is ready'; }
  waitfor 120 "the update ready" ready
  [[ $(fetched android-amd64.apk) == 1 ]] || fail "the updater fetched the APK $(fetched android-amd64.apk) times"
  adb shell input keyevent KEYCODE_HOME
  sleep 2
  offered() { shows "Dens v99.0.2-e2e is ready to install"; }
  waitfor 20 "the update's notification" offered
  [[ $(notifications) == *"pkg=$RELEASE_APP"*"channel=updates"* ]] || fail "the update's notification isn't the app's, in its Updates channel"
  ok "the updater checked v99.0.2-e2e's APK against its signed checksums, and the shell posted that it's ready"

  adb shell cmd statusbar expand-notifications
  sleep 2
  # On its own, not in the bundle Android makes of an app's quiet
  # notifications, whose tap would only open the app.
  tap 10 'Dens v99.0.2-e2e is ready to install' || fail "no update's notification to tap in the shade"
  handed() { release_log | grep -q 'update: handed v99.0.2-e2e to Android'"'"'s installer'; }
  waitfor 30 "the APK handed to Android's installer" handed
  tap 30 'Update|UPDATE|Install|INSTALL' || fail "Android's installer asked nothing to confirm"
  updated() { [[ $(version_of) == v99.0.2-e2e ]]; }
  waitfor 120 "the app updated to v99.0.2-e2e" updated
  up() { release_log | grep -q 'service: Dens v99.0.2-e2e is up'; }
  waitfor 60 "v99.0.2-e2e's Dens up" up
  release_log | grep -q 'start: MY_PACKAGE_REPLACED' || fail "Android didn't start the updated app's service"
  ok "the notification's tap handed the APK to Android's installer, which updated the app to v99.0.2-e2e, and Dens started again"
  incus exec "$CONTAINER" -- cp /root/releases.log /root/releases-host.log || :
  incus file pull -q "$CONTAINER/root/releases-host.log" "$RUN_LOG_DIR/releases-host.log" || :
  stop_releases_host
  adb reverse --remove "tcp:$RELEASES_PORT" >/dev/null 2>&1 || :
  adb uninstall "$RELEASE_APP" >/dev/null 2>&1 || :
}
update_from_fixtures
incus exec "$CONTAINER" -- /root/box.sh halt 5554

echo ">> Android 12"
serial=$OLD
incus exec "$CONTAINER" -- /root/box.sh boot a12 5556
adb uninstall "$APP" >/dev/null 2>&1 || :
phone install amd64 >/dev/null
phone start
waitfor 60 "Dens on Android 12" sh -c "scripts/test/android/phone.sh $serial log -s Dens:* | grep -q 'Dens v.* is up'"
waitfor 10 "the WebView's note" sh -c "scripts/test/android/phone.sh $serial log -s Dens:* | grep -q 'the WebView is [0-9]*, older than'"
ok "on Android 12 Dens runs, and the app says to update the WebView, $(shell_log | sed -n 's/.*the WebView is \([0-9]*\), older.*/\1/p' | head -1)"
update_from_fixtures

passed=true
