#!/usr/bin/env bash
# The Relay spike's Android app: this tree's development build, its shell
# changed for questions 8 and 9 and the probe beside Dens in it. The shell
# under android/ stays as it is: a copy takes Relay.kt and four edits,
# each of which must find its line. The APKs land where a development
# build's do, out/android-{arm64,amd64}.apk, so scripts/test/android/phone.sh
# installs them.
#
# Usage: spikes/relay/android/build.sh

set -euo pipefail
cd "$(dirname "$0")/../../.."
# shellcheck source=/dev/null
source scripts/build.sh

SPIKE=spikes/relay/android
COPY=spikes/relay/bin/android-src
rm -rf "$COPY"
mkdir -p "$(dirname "$COPY")"
cp -r android "$COPY"
cp "$SPIKE/Relay.kt" "$COPY/src/chat/dens/shell/Relay.kt"
python3 -I - "$COPY" <<'PY'
import sys
root = sys.argv[1]

def edit(path, old, new):
    with open(f'{root}/{path}') as f:
        text = f.read()
    if text.count(old) != 1:
        sys.exit(f'{path}: expected one {old!r}, found {text.count(old)}')
    with open(f'{root}/{path}', 'w') as f:
        f.write(text.replace(old, new))

# Media playback's type for the service, and the locks' permission.
edit('AndroidManifest.xml',
     '    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_SPECIAL_USE" />\n',
     '    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_SPECIAL_USE" />\n'
     '    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_MEDIA_PLAYBACK" />\n'
     '    <uses-permission android:name="android.permission.WAKE_LOCK" />\n'
     # Android 17 keeps an app off its own network without this one, which
     # the member allows as they do the microphone: the probe's first
     # packet to the other side fails with EPERM.
     '    <uses-permission android:name="android.permission.ACCESS_LOCAL_NETWORK" />\n')
edit('AndroidManifest.xml', 'android:foregroundServiceType="specialUse|microphone"',
     'android:foregroundServiceType="specialUse|microphone|mediaPlayback"')
edit('src/chat/dens/shell/DensService.kt',
     '            startForeground(NOTIFICATION, notification(), base or mic)\n',
     '            startForeground(NOTIFICATION, notification(), base or mic or\n'
     '                (if (Relay.on) ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK else 0))\n')
# adb's word to start or stop a relay, as the activity opens and after.
edit('src/chat/dens/shell/PageActivity.kt',
     '        installTapped(intent)\n    }\n',
     '        installTapped(intent)\n        Relay.asked(this, root, intent)\n    }\n')
edit('src/chat/dens/shell/PageActivity.kt',
     '        super.onNewIntent(intent)\n',
     '        super.onNewIntent(intent)\n'
     '        if (intent.hasExtra("relay")) return Relay.asked(this, root, intent)\n')
edit('src/chat/dens/shell/PageActivity.kt',
     '        DensService.unwatch(watcher)\n',
     '        DensService.unwatch(watcher)\n        Relay.stop(this, null)\n')
PY

# shellcheck disable=SC2034  # android_apks reads it
ANDROID_SOURCE="./$COPY"

# The probe goes into each APK as Dens does, a library to Android's eyes,
# built the same way: android_apks adds Dens last, and the probe with it.
eval "$(declare -f android_add | sed '1s/android_add/android_add_dens/')"
android_add() {
  android_add_dens "$@"
  [[ "${3:-}" == lib/*/libdens.so ]] || return 0
  local abi=${3#lib/} goarch triple target
  abi=${abi%/libdens.so}
  for target in "${ANDROID_TARGETS[@]}"; do
    [[ "$target" == *" $abi "* ]] && read -r goarch _ triple <<<"$target"
  done
  local out
  out=$(vendor_abs "$2")/lib/$abi/librelayprobe.so
  (cd spikes && env GOOS=android GOARCH="$goarch" CGO_ENABLED=1 CC="$ANDROID_NDK_BIN/$triple$ANDROID_MIN_SDK-clang" \
    go build -trimpath -buildvcs=false -ldflags="-s -w -checklinkname=0" -o "$out" ./relay)
  android_check_alignment "$ANDROID_NDK_BIN/llvm-readelf" "$out"
  android_add_dens "$1" "$2" "lib/$abi/librelayprobe.so"
  printf "🟢 Added the probe for %s\n" "$abi"
}

# What build.sh does before an Android build, without its arguments.
# shellcheck disable=SC2034  # build.sh's steps read it
BUILD_KIND=android
validate_app_name
detect_host_arch
validate_pins
dep_check
android_build
