#!/usr/bin/env bash
# Builds the Android spike: Dens and the probe for Android, through the NDK
# here, and the shell's APK around them in an Incus container
# (dens-android-spike, Debian 13) that holds the SDK's pieces, Kotlin and
# the emulator, with /dev/kvm passed in.
#
# Usage: spikes/android/build.sh             the container, the binaries and both APKs
#        spikes/android/build.sh binaries    Dens and the probe, for arm64 and x86_64
#        spikes/android/build.sh apk [CODE]  the APKs again, as version CODE
#        spikes/android/build.sh container   the container and its SDK
#
# The APKs land in out/android-spike: dens-spike-arm64-v8a.apk for the
# phones, dens-spike-x86_64.apk for the emulator. Each carries a
# development build of Dens, the real client and page, since the Android
# form doesn't exist yet, stripped as a release is. They're signed with a
# throwaway key kept in out/android-spike/keys and in the container, which
# every later build must carry for the phones to take it as an update.

set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$PWD
OUT=$ROOT/out/android-spike
CONTAINER=${CONTAINER:-dens-android-spike}
NDK_ZIP=android-ndk-r30-linux.zip
NDK_SHA256=753611f410d002cfcd3f3dc2ef49aad532089d3180b436c060a90bf0fcb64df2
# Android 12, the oldest the shell runs on, whose NDK toolchain bionic
# links against.
API=31
ABIS=(arm64-v8a x86_64)

in_container() { incus exec "$CONTAINER" -- "$@"; }

container() {
  if ! incus info "$CONTAINER" >/dev/null 2>&1; then
    incus launch dens-e2e/debian-13 "$CONTAINER" >/dev/null
    for _ in $(seq 60); do
      in_container getent hosts deb.debian.org >/dev/null 2>&1 && break
      sleep 1
    done
  fi
  incus info "$CONTAINER" | grep -q '^Status: RUNNING' || incus start "$CONTAINER"
  # The emulator runs as root in the container, which opens the device.
  incus config device show "$CONTAINER" | grep -q '^kvm:' ||
    incus config device add "$CONTAINER" kvm unix-char source=/dev/kvm path=/dev/kvm uid=0 gid=0 mode=0666 >/dev/null
  local packages=(openjdk-21-jdk-headless unzip zip bc curl ca-certificates python3 procps file
    libpulse0 libgl1 libnss3 libxcomposite1 libxcursor1 libxdamage1 libxi6 libxtst6 libxrandr2 libxkbfile1
    libasound2t64 libdbus-1-3 libfontconfig1 libxkbcommon0 libx11-xcb1 libbsd0)
  if ! in_container dpkg-query -W "${packages[@]}" >/dev/null 2>&1; then
    in_container sh -c "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends ${packages[*]} >/dev/null"
  fi
  incus file push -q --mode 0755 spikes/android/box.sh "$CONTAINER/root/box.sh"
  in_container /root/box.sh sdk
  # The key outlives the container: a phone that installed from one takes
  # updates signed by it alone.
  if [[ -f $OUT/keys/spike.p12 ]] && ! in_container test -f /root/spike/keys/spike.p12; then
    in_container mkdir -p /root/spike/keys
    incus file push -q "$OUT/keys/spike.p12" "$CONTAINER/root/spike/keys/spike.p12"
  fi
}

ndk() {
  # shellcheck source=scripts/build/common.sh
  source scripts/build/common.sh
  mkdir -p tools
  download_verified "tools/$NDK_ZIP" "https://dl.google.com/android/repository/$NDK_ZIP" "$NDK_SHA256" "Android NDK"
  if [[ ! -d tools/android-ndk-r30 ]]; then
    rm -rf tools/.ndk-extract
    unzip -q "tools/$NDK_ZIP" -d tools/.ndk-extract
    mv tools/.ndk-extract/android-ndk-r30 tools/android-ndk-r30
    rmdir tools/.ndk-extract
  fi
  NDK_BIN=$ROOT/tools/android-ndk-r30/toolchains/llvm/prebuilt/linux-x86_64/bin
}

# binaries builds the page's assets, then Dens and the probe for each ABI:
# cgo through the NDK, for bionic's resolver, and -checklinkname=0, which
# Pion's interface listing (wlynxg/anet) needs.
binaries() {
  ndk
  (
    # shellcheck source=scripts/build.sh
    source scripts/build.sh
    detect_host_arch
    frontend_build
    frontend_hash_assets
    local abi goarch cc ldflags
    ldflags="$(make_ldflags true) -s -w -checklinkname=0"
    for abi in "${ABIS[@]}"; do
      case $abi in
        arm64-v8a) goarch=arm64 cc=aarch64-linux-android$API-clang ;;
        x86_64) goarch=amd64 cc=x86_64-linux-android$API-clang ;;
      esac
      mkdir -p "$OUT/lib/$abi"
      export GOOS=android GOARCH=$goarch CGO_ENABLED=1 CC=$NDK_BIN/$cc GOTOOLCHAIN=go1.26.7
      go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$OUT/lib/$abi/libdens.so" ./cmd
      (cd spikes && go build -trimpath -buildvcs=false -ldflags="-s -w -checklinkname=0 -X main.version=$(date +%m%d.%H%M)" -o "$OUT/lib/$abi/libprobe.so" ./android)
      printf '🟢 Built %s: %s\n' "$abi" "$(cd "$OUT/lib/$abi" && stat -c '%n %s bytes' libdens.so libprobe.so | paste -sd ' ')"
    done
  )
}

# apk CODE builds both APKs in the container as version CODE.
apk() {
  local code=${1:-1}
  in_container rm -rf /root/spike/app /root/spike/lib
  in_container mkdir -p /root/spike/out
  incus file push -q -r spikes/android/app "$CONTAINER/root/spike/"
  incus file push -q -r "$OUT/lib" "$CONTAINER/root/spike/"
  incus file push -q --mode 0755 spikes/android/box.sh "$CONTAINER/root/box.sh"
  incus file push -q spikes/android/apicheck.py "$CONTAINER/root/spike/apicheck.py"
  for abi in "${ABIS[@]}"; do
    in_container /root/box.sh apk "$abi" "$code"
    incus file pull -q "$CONTAINER/root/spike/out/dens-spike-$abi.apk" "$OUT/"
  done
  mkdir -p "$OUT/keys"
  [[ -f $OUT/keys/spike.p12 ]] || incus file pull -q "$CONTAINER/root/spike/keys/spike.p12" "$OUT/keys/"
  ls -l "$OUT"/*.apk
}

case ${1:-} in
  "")
    container
    binaries
    apk
    ;;
  binaries) binaries ;;
  apk) apk "${2:-1}" ;;
  container) container ;;
  *)
    sed -n '7,10p' "$0" >&2
    exit 2
    ;;
esac
