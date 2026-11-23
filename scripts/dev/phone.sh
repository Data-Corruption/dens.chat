#!/usr/bin/env bash
# Puts a staged manual test's Dens dev on a phone over wireless debugging,
# through the Android container's adb, over the Dens dev there, keeping
# its data. stage.sh copies this beside the APK it stages, and it runs from
# there, wherever the folder goes.
#
# Usage: phone.sh ADDRESS:PORT             connect to the phone and install the app
#        phone.sh pair ADDRESS:PORT CODE   pair with the phone first
#
# Both addresses are on the phone, under Settings, Developer options,
# Wireless debugging. Installing takes the one that screen shows, under IP
# address & Port. Pairing takes those of Pair device with pairing code: its
# own address and port, and its six digits. A phone needs pairing once, and
# again after it forgets this computer, as when its debugging is reset.

set -euo pipefail
CONTAINER=${CONTAINER:-dens-android}
here=$(cd "$(dirname "$0")" && pwd)
usage() { awk '/^# Usage:/ { on = 1 } on && !/^#/ { exit } on' "$0" >&2; exit 2; }
adb() { incus exec "$CONTAINER" -- /opt/android/sdk/platform-tools/adb "$@" </dev/null; }

[[ $# -ge 1 ]] || usage
if [[ $(incus list "$CONTAINER" --format csv -c s) != RUNNING ]]; then
  incus start "$CONTAINER"
fi

if [[ $1 == pair ]]; then
  [[ $# -eq 3 ]] || usage
  adb pair "$2" "$3"
  echo "Paired. Now install: bash $0 ADDRESS:PORT, with the address Wireless debugging itself shows."
  exit 0
fi

[[ $# -eq 1 ]] || usage
serial=$1
adb connect "$serial"
# adb connect succeeds even when the phone turns it away.
if [[ $(adb -s "$serial" get-state 2>/dev/null) != device ]]; then
  cat >&2 <<EOF
The phone at $serial didn't take the connection. If the address is the one
Wireless debugging shows now, it wants pairing: tap Pair device with pairing
code there, then
  bash $0 pair ADDRESS:PORT CODE
with the address and the six digits that dialog shows.
EOF
  exit 1
fi
incus file push -q "$here/android-arm64.apk" "$CONTAINER/root/android-arm64.apk" </dev/null
adb -s "$serial" install -r --no-incremental /root/android-arm64.apk
echo "Installed. Open Dens dev on the phone."
