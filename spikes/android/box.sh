#!/bin/sh
# Runs in the spike's container, as root: the Android SDK's pieces from
# their pinned downloads, the APK from the shell's sources and the
# binaries build.sh pushes, and the emulator's devices.
#
# Usage: box.sh sdk                 unpack the SDK, the NDK-free part
#        box.sh apk ABI CODE        build /root/spike/out/dens-spike-ABI.apk
#        box.sh avd NAME IMAGE      make an emulator device from a system image
set -eu

A=/opt/android
D=$A/downloads
SDK=$A/sdk
export ANDROID_HOME=$SDK

# Every download, by the SHA-256 it was pinned at (Google publishes SHA-1s,
# which each matched when these were taken on 2026-10-26).
R=https://dl.google.com/android/repository
pins() {
  cat <<PINS
build-tools_r37_linux.zip $R/build-tools_r37_linux.zip 01af179347cbcd9c208b7f8171f7b21f6dd1d2f85bcd15e88caa51d5d7b86060
platform-37.0_r02.zip $R/platform-37.0_r02.zip 840b23e827f96e64aea4c89a1194aac3dc5f6bad37edb231c5c795d890330e8d
platform-tools_r37.0.1-linux.zip $R/platform-tools_r37.0.1-linux.zip d230f13842f60f782a8645f9c813f8f845bf36089ea7289f28c48f17979313f1
emulator-linux_x64-16428233.zip $R/emulator-linux_x64-16428233.zip b08fc43d8608d2955f607f1b287beb525041e730086bdca3af152accac3af9c1
x86_64-37.0_r08.zip $R/sys-img/google_apis/x86_64-37.0_r08.zip 75ed072b88f9bb7edde0e3d22427deddbdad8be168b058a79269b55de40f6f25
x86_64-ps16k-37.0_r08.zip $R/sys-img/google_apis/x86_64-ps16k-37.0_r08.zip 94936d2fb7d8bd1d0c2411510ca7e06ce93184695cad8a43fea036660ffb972d
x86_64-31_r14.zip $R/sys-img/google_apis/x86_64-31_r14.zip 7e7081f5784e98dd391ddae52573a75bc1db17a2fd286cb20be46d3eec251f94
kotlin-compiler-2.4.21.zip https://github.com/JetBrains/kotlin/releases/download/v2.4.21/kotlin-compiler-2.4.21.zip 7cc140e76daf416a0424a5557f99afd7ca89ebd463ea1101261e3e01c33e75cd
PINS
}

fetch() {
  mkdir -p "$D"
  pins | while read -r file url sum; do
    if [ ! -f "$D/$file" ]; then
      curl -fsSL --retry 3 --connect-timeout 30 -o "$D/$file.part" "$url"
      mv "$D/$file.part" "$D/$file"
    fi
    echo "$sum  $D/$file" | sha256sum -c --quiet
  done
}

# unpack ZIP DIR TOP puts the archive's top directory TOP at DIR.
unpack() {
  [ -d "$2" ] && return 0
  rm -rf "$A/.unpack"
  mkdir -p "$A/.unpack" "$(dirname "$2")"
  unzip -q "$D/$1" -d "$A/.unpack"
  mv "$A/.unpack/$3" "$2"
  rm -rf "$A/.unpack"
}

sdk() {
  fetch
  unpack build-tools_r37_linux.zip "$SDK/build-tools/37.0.0" android-37.0
  unpack platform-37.0_r02.zip "$SDK/platforms/android-37.0" android-37.0
  unpack platform-tools_r37.0.1-linux.zip "$SDK/platform-tools" platform-tools
  unpack emulator-linux_x64-16428233.zip "$SDK/emulator" emulator
  unpack x86_64-37.0_r08.zip "$SDK/system-images/android-37.0/google_apis/x86_64" x86_64
  unpack x86_64-ps16k-37.0_r08.zip "$SDK/system-images/android-37.0/google_apis_ps16k/x86_64" x86_64
  unpack x86_64-31_r14.zip "$SDK/system-images/android-31/google_apis/x86_64" x86_64
  unpack kotlin-compiler-2.4.21.zip "$A/kotlinc" kotlinc
  "$SDK/platform-tools/adb" version | head -1
  "$SDK/build-tools/37.0.0/aapt2" version
  "$A/kotlinc/bin/kotlinc" -version 2>&1 | tail -1
  "$SDK/emulator/emulator" -version 2>/dev/null | head -1
}

# avd NAME IMAGE makes an emulator device from the system image at
# system-images/IMAGE (android-37.0/google_apis/x86_64, say), with a
# phone's screen: 1080 by 2400 at 420 dpi, 411 pixels wide to the page.
avd() {
  dir=/root/.android/avd/$1.avd
  mkdir -p "$dir"
  printf 'avd.ini.encoding=UTF-8\npath=%s\npath.rel=avd/%s.avd\ntarget=android-37\n' "$dir" "$1" >"/root/.android/avd/$1.ini"
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

# apk ABI CODE builds the shell for one ABI as version CODE: resources and
# the manifest through aapt2, Kotlin through kotlinc and d8 with Kotlin's
# standard library, the binaries in lib/ABI, then zipalign and apksigner,
# each timed.
apk() {
  abi=$1 code=$2
  bt=$SDK/build-tools/37.0.0
  jar=$SDK/platforms/android-37.0/android.jar
  w=/root/spike/build/$abi
  rm -rf "$w"
  mkdir -p "$w/dex" "$w/files/lib/$abi" /root/spike/out /root/spike/keys
  step() {
    label=$1
    shift
    t0=$(date +%s.%N)
    "$@"
    printf '  %-10s %5.1f s\n' "$label" "$(echo "$(date +%s.%N) - $t0" | bc)"
  }
  echo "dens-spike-$abi.apk, version $code:"
  step aapt2 sh -c "$bt/aapt2 compile --dir /root/spike/app/res -o $w/res.zip &&
    $bt/aapt2 link -o $w/unsigned.apk -I $jar --manifest /root/spike/app/AndroidManifest.xml \
      --min-sdk-version 31 --target-sdk-version 37 --version-code $code --version-name 0.$code $w/res.zip"
  step kotlinc "$A/kotlinc/bin/kotlinc" /root/spike/app/src -cp "$jar" -jvm-target 17 -no-reflect -nowarn -d "$w/classes.jar"
  step d8 "$bt/d8" --release --min-api 31 --lib "$jar" --output "$w/dex" "$w/classes.jar" "$A/kotlinc/lib/kotlin-stdlib.jar"
  # No Gradle means no Android Lint: this lists the calls newer than the
  # minSdk, which need an SDK_INT check around them.
  python3 -I /root/spike/apicheck.py "$SDK/platforms/android-37.0/data/api-versions.xml" "$w/classes.jar" 31 | sed 's/^/  newer than 31: /'
  cp /root/spike/lib/"$abi"/*.so "$w/files/lib/$abi/"
  step zip sh -c "cd $w/dex && zip -q $w/unsigned.apk classes*.dex && cd $w/files && zip -q -r $w/unsigned.apk lib"
  step zipalign "$bt/zipalign" -f -p 4 "$w/unsigned.apk" "$w/aligned.apk"
  if [ ! -f /root/spike/keys/spike.p12 ]; then
    keytool -genkeypair -keystore /root/spike/keys/spike.p12 -storetype PKCS12 -storepass spike-key -alias spike \
      -keyalg EC -groupname secp256r1 -validity 3650 -dname "CN=Dens Spike" >/dev/null 2>&1
  fi
  step apksigner "$bt/apksigner" sign --ks /root/spike/keys/spike.p12 --ks-pass pass:spike-key \
    --out "/root/spike/out/dens-spike-$abi.apk" "$w/aligned.apk"
  stat -c '  %s %n' "$w"/dex/*.dex "/root/spike/out/dens-spike-$abi.apk"
}

# variant NAME ABI CODE builds a cut-down spike for Play Protect's verdict,
# from the last apk's dex: chat.dens.spike.NAME, labelled Dens Spike NAME,
# not debuggable, with Dens alone in lib/ABI, as the real app will carry.
# NAME b also leaves out the permissions to install packages; c keeps
# them; d keeps REQUEST_INSTALL_PACKAGES alone, for updates the member taps
# through Android's installer. Since the S24's verdict the shell itself
# goes without UPDATE_PACKAGES_WITHOUT_USER_ACTION, so c is d.
variant() {
  name=$1 abi=$2 code=$3
  bt=$SDK/build-tools/37.0.0
  jar=$SDK/platforms/android-37.0/android.jar
  src=/root/spike/build/$abi
  w=/root/spike/build/variant-$name
  rm -rf "$w"
  mkdir -p "$w/files/lib/$abi"
  sed -e '/android:debuggable/d' -e "s/android:label=\"Dens Spike\"/android:label=\"Dens Spike $name\"/" \
    /root/spike/app/AndroidManifest.xml >"$w/AndroidManifest.xml"
  case $name in
    b) sed -i -e '/REQUEST_INSTALL_PACKAGES/d' -e '/UPDATE_PACKAGES_WITHOUT_USER_ACTION/d' "$w/AndroidManifest.xml" ;;
    d) sed -i -e '/UPDATE_PACKAGES_WITHOUT_USER_ACTION/d' "$w/AndroidManifest.xml" ;;
  esac
  "$bt/aapt2" compile --dir /root/spike/app/res -o "$w/res.zip"
  "$bt/aapt2" link -o "$w/unsigned.apk" -I "$jar" --manifest "$w/AndroidManifest.xml" \
    --rename-manifest-package "chat.dens.spike.$name" \
    --min-sdk-version 31 --target-sdk-version 37 --version-code "$code" --version-name "0.$code" "$w/res.zip"
  cp "$src/dex/classes.dex" "$w/"
  cp /root/spike/lib/"$abi"/libdens.so "$w/files/lib/$abi/"
  (cd "$w" && zip -q unsigned.apk classes.dex && cd files && zip -q -r ../unsigned.apk lib)
  "$bt/zipalign" -f -p 4 "$w/unsigned.apk" "$w/aligned.apk"
  "$bt/apksigner" sign --ks /root/spike/keys/spike.p12 --ks-pass pass:spike-key \
    --out "/root/spike/out/dens-spike-$name-$abi.apk" "$w/aligned.apk"
  stat -c '  %s %n' "/root/spike/out/dens-spike-$name-$abi.apk"
}

case ${1:-} in
  sdk) sdk ;;
  variant) variant "$2" "$3" "$4" ;;
  apk) apk "$2" "$3" ;;
  avd) avd "$2" "$3" ;;
  *) sed -n '6,9p' "$0" >&2; exit 2 ;;
esac
