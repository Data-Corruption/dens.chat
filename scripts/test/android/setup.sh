#!/usr/bin/env bash
# Sets up the Android e2e's Incus container, or brings one up to date
# (M12): Debian 13 with /dev/kvm and the emulator's libraries; the SDK's
# platform tools (adb), the emulator and its system images, from the
# downloads vendor.sh verified; the emulator's devices, a17 on Android 17
# and a12 on Android 12, the oldest the app runs on; the pinned Node.js;
# and this directory's drivers. Its adb is also how phone.sh reaches a
# phone over wireless debugging, which keeps its pairing.
#
# Usage: scripts/test/android/setup.sh
#
# Needs a local Incus daemon, and KVM. CONTAINER names the container
# (dens-android).

set -euo pipefail
cd "$(dirname "$0")/../../.."

CONTAINER=${CONTAINER:-dens-android}
in_box() { incus exec "$CONTAINER" -- "$@"; }

[[ -e /dev/kvm ]] || { echo "error: the emulator needs /dev/kvm" >&2; exit 1; }
if ! incus info "$CONTAINER" >/dev/null 2>&1; then
  image=images:debian/13
  incus image info dens-e2e/debian-13 >/dev/null 2>&1 && image=dens-e2e/debian-13
  echo "Creating $CONTAINER from $image ..."
  incus launch "$image" "$CONTAINER" >/dev/null
  for _ in $(seq 60); do
    in_box getent hosts deb.debian.org >/dev/null 2>&1 && break
    sleep 1
  done
fi
# Each check reads incus's answer whole: grep -q quits at its match, which
# can leave incus writing to a closed pipe, and pipefail counts that as
# the check failing.
grep -q '^Status: RUNNING' <<<"$(incus info "$CONTAINER")" || incus start "$CONTAINER"
# The emulator runs as root in the container, which opens the device.
grep -q '^kvm:' <<<"$(incus config device show "$CONTAINER")" ||
  incus config device add "$CONTAINER" kvm unix-char source=/dev/kvm path=/dev/kvm uid=0 gid=0 mode=0666 >/dev/null

packages=(unzip curl ca-certificates python3 procps file
  libpulse0 libgl1 libnss3 libxcomposite1 libxcursor1 libxdamage1 libxi6 libxtst6 libxrandr2 libxkbfile1
  libasound2t64 libdbus-1-3 libfontconfig1 libxkbcommon0 libx11-xcb1 libbsd0)
if ! in_box dpkg-query -W "${packages[@]}" >/dev/null 2>&1; then
  in_box sh -c "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends ${packages[*]} >/dev/null"
fi

paths=$(./scripts/vendor.sh android-emulator node)
downloads=$(sed -n 's/^android-emulator=//p' <<<"$paths")
node=$(sed -n 's/^node=//p' <<<"$paths")
in_box mkdir -p /opt/android/downloads
# The system images are 2 GB each: one already there, at the size vendor.sh
# verified, stays.
for file in "$downloads"/*.zip; do
  name=$(basename "$file")
  size=$(stat -c %s "$file")
  if [[ "$(in_box stat -c %s "/opt/android/downloads/$name" 2>/dev/null)" != "$size" ]]; then
    echo "Pushing $name ..."
    incus file push -q "$file" "$CONTAINER/opt/android/downloads/$name"
  fi
done
incus file push -q --mode 0755 scripts/test/android/box.sh "$CONTAINER/root/box.sh"
in_box /root/box.sh sdk
in_box /root/box.sh avd a17 android-37.0/google_apis/x86_64
in_box /root/box.sh avd a12 android-31/google_apis/x86_64
incus file push -q --mode 0755 "$node" "$CONTAINER/root/node"
incus file push -q scripts/test/android/drive.mjs scripts/test/android/check.mjs scripts/test/android/call.mjs "$CONTAINER/root/"
echo "$CONTAINER is ready"
