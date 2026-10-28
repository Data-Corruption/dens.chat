#!/bin/sh
# Runs in the Android container, as root (M12): the SDK's pieces the e2e
# needs, unpacked from the downloads setup.sh pushes once vendor.sh has
# verified them; the emulator's devices; and the emulator.
#
# Usage: box.sh sdk                 unpack /opt/android/downloads into the SDK
#        box.sh avd NAME IMAGE      make an emulator's device from a system image
#        box.sh boot NAME PORT      start the emulator on NAME, and wait for Android
#        box.sh halt PORT           stop the emulator on PORT
set -eu

A=/opt/android
D=$A/downloads
SDK=$A/sdk
export ANDROID_HOME=$SDK ANDROID_SDK_ROOT=$SDK

# unpack ZIP DIR TOP puts the archive's top directory TOP at DIR, once: DIR
# keeps the archive's name, so a new pin unpacks again.
unpack() {
  [ "$(cat "$2/.unpacked" 2>/dev/null)" = "$1" ] && return 0
  rm -rf "$A/.unpack" "$2"
  mkdir -p "$A/.unpack" "$(dirname "$2")"
  unzip -q "$D/$1" -d "$A/.unpack"
  mv "$A/.unpack/$3" "$2"
  rm -rf "$A/.unpack"
  echo "$1" >"$2/.unpacked"
}

sdk() {
  unpack platform-tools_r37.0.1-linux.zip "$SDK/platform-tools" platform-tools
  unpack emulator-linux_x64-16428233.zip "$SDK/emulator" emulator
  unpack x86_64-37.0_r08.zip "$SDK/system-images/android-37.0/google_apis/x86_64" x86_64
  unpack x86_64-31_r14.zip "$SDK/system-images/android-31/google_apis/x86_64" x86_64
  "$SDK/platform-tools/adb" version | head -1
  "$SDK/emulator/emulator" -version 2>/dev/null | head -1
}

# avd NAME IMAGE makes an emulator's device from the system image at
# system-images/IMAGE (android-37.0/google_apis/x86_64, say), once, with a
# phone's screen: 1080 by 2400 at 420 dpi, 411 pixels wide to the page.
avd() {
  dir=/root/.android/avd/$1.avd
  [ -f "$dir/config.ini" ] && return 0
  mkdir -p "$dir"
  printf 'avd.ini.encoding=UTF-8\npath=%s\npath.rel=avd/%s.avd\n' "$dir" "$1" >"/root/.android/avd/$1.ini"
  cat >"$dir/config.ini" <<CONFIG
AvdId=$1
avd.ini.displayname=$1
avd.ini.encoding=UTF-8
abi.type=x86_64
hw.cpu.arch=x86_64
hw.cpu.ncore=4
hw.ramSize=4096
vm.heapSize=512
disk.dataPartition.size=8G
hw.lcd.width=1080
hw.lcd.height=2400
hw.lcd.density=420
hw.keyboard=yes
hw.mainKeys=no
hw.gpu.enabled=yes
hw.gpu.mode=swiftshader_indirect
hw.audioInput=yes
hw.audioOutput=yes
hw.camera.back=none
hw.camera.front=none
hw.sdCard=no
PlayStore.enabled=false
image.sysdir.1=system-images/$2/
tag.id=$(echo "$2" | cut -d/ -f2)
CONFIG
  echo "made $1 from $2"
}

booted() {
  [ "$("$SDK/platform-tools/adb" -s "emulator-$1" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = 1 ]
}

# boot NAME PORT starts the emulator on device NAME, headless, with
# software graphics, unless it's up, and waits for Android to finish
# starting.
boot() {
  if booted "$2"; then
    echo "$1 is up"
    return 0
  fi
  nohup "$SDK/emulator/emulator" -avd "$1" -no-window -no-audio -no-snapshot -no-boot-anim \
    -gpu swiftshader_indirect -port "$2" >"/root/emulator-$1.log" 2>&1 &
  i=0
  until booted "$2"; do
    i=$((i + 1))
    if [ "$i" -gt 300 ]; then
      echo "$1 didn't start in 5 minutes:"
      tail -n 20 "/root/emulator-$1.log"
      exit 1
    fi
    sleep 1
  done
  echo "$1 started in $i s"
}

halt() {
  "$SDK/platform-tools/adb" -s "emulator-$1" emu kill >/dev/null 2>&1 || true
}

case ${1:-} in
  sdk) sdk ;;
  avd) avd "$2" "$3" ;;
  boot) boot "$2" "$3" ;;
  halt) halt "$2" ;;
  *)
    sed -n '6,9p' "$0" >&2
    exit 2
    ;;
esac
