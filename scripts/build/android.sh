# The Android app (M12): Dens built for Android with cgo through the NDK,
# and the APK around it, built without Gradle from the SDK's aapt2, d8,
# zipalign and apksigner, Kotlin's compiler and a JDK, as vendor.sh pins
# them. Sourced by ../build.sh.
#
# A development build is chat.dens.Dens.dev, labelled Dens (development),
# debuggable, and signed with a key of the developer's own, apart from the
# release's, so Play Protect's verdict on one never reaches the other.

# Each ABI the APKs carry Dens for: Go's name for it, Android's, and the
# NDK's clang that builds for it. The phones are arm64, the emulator x86_64.
ANDROID_TARGETS=(
  "arm64 arm64-v8a aarch64-linux-android"
  "amd64 x86_64 x86_64-linux-android"
)
ANDROID_SOURCE="./android"
ANDROID_DEV_PACKAGE="chat.dens.Dens.dev"
# The Android the app is built for, 17, whose platform vendor.sh pins, and
# the oldest it runs on, 12, which the NDK builds Dens against.
ANDROID_TARGET_SDK=37
ANDROID_MIN_SDK=31

# android_dev_key makes the development APKs' signing key once, outside the
# repository and out/, which every build empties: a phone takes an update
# only signed with the key its first install was.
android_dev_key() {
  local dir="${XDG_CONFIG_HOME:-$HOME/.config}/dens-dev"
  ANDROID_DEV_KEY="$dir/android.p12"
  [[ -f "$ANDROID_DEV_KEY" ]] && return 0
  mkdir -p "$dir"
  keytool -genkeypair -keystore "$ANDROID_DEV_KEY" -storetype PKCS12 -storepass dens-dev -alias dens-dev \
    -keyalg EC -groupname secp256r1 -validity 10950 -dname "CN=Dens development" >/dev/null 2>&1
  chmod 0600 "$ANDROID_DEV_KEY"
  printf "🟢 Made the development APKs' signing key in %s, which every later one must carry\n" "$ANDROID_DEV_KEY"
}

# android_check_alignment READELF BINARY refuses a binary with a loadable
# segment aligned for less than 16 KB pages, which Android 15 and later
# may use.
android_check_alignment() {
  local align
  for align in $("$1" -lW "$2" | awk '$1 == "LOAD" { print $NF }'); do
    if (( align < 0x4000 )); then
      printf "🔴 %s has a segment aligned at %s, not for 16 KB pages\n" "$2" "$align" >&2
      return 1
    fi
  done
}

# android_add APK DIR NAME... adds DIR/NAME to the APK as NAME, compressed,
# with a fixed time, so the same files make the same entries.
android_add() {
  python3 -I - "$@" <<'PY'
import sys, zipfile
apk, base = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(apk, 'a') as z:
    for name in sys.argv[3:]:
        info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
        info.compress_type = zipfile.ZIP_DEFLATED
        info.external_attr = 0o644 << 16
        with open(base + '/' + name, 'rb') as f:
            z.writestr(info, f.read())
PY
}

# android_build builds the development APKs: out/android-arm64.apk for
# phones, and out/android-amd64.apk for the emulator.
android_build() {
  vendor_android
  vendor_require_bins python3 unzip
  local tools ndk_bin sdk build_tools platform
  tools=$(vendor_abs "$VENDOR_ANDROID")
  ndk_bin="$tools/ndk/toolchains/llvm/prebuilt/linux-x86_64/bin"
  sdk="$tools/sdk"
  build_tools="$sdk/build-tools/$ANDROID_BUILD_TOOLS_DIR"
  platform="$sdk/platforms/$ANDROID_PLATFORM"
  export JAVA_HOME="$tools/jdk"
  export PATH="$JAVA_HOME/bin:$PATH"

  # Its own directory: out/android holds what phone.sh and bg.sh keep of
  # each device.
  local work="$OUT_DIR/android-build"
  rm -rf "$work" "$OUT_DIR"/android-*.apk*
  mkdir -p "$work/dex"
  frontend_build
  frontend_hash_assets

  # A development build's flags, stripped as a release's are: the APK goes
  # over wireless debugging, and nothing debugs Dens on a phone with
  # symbols. -checklinkname=0 lets Pion's interface listing (wlynxg/anet)
  # reach what Android's netlink rules leave it.
  local ldflags target goarch abi triple
  ldflags="$(make_ldflags "$DEV_MODE") -s -w -checklinkname=0"
  for target in "${ANDROID_TARGETS[@]}"; do
    read -r goarch abi triple <<<"$target"
    mkdir -p "$work/lib/$abi"
    # cgo, so names resolve through Android's own resolver, which keeps
    # its Private DNS: Go's finds no resolver there. The NDK's clang for
    # the oldest Android the app runs on.
    run_step "Built Dens for Android $goarch" "Building Dens for Android $goarch failed" \
      env GOOS=android GOARCH="$goarch" CGO_ENABLED=1 CC="$ndk_bin/$triple$ANDROID_MIN_SDK-clang" \
      go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$work/lib/$abi/libdens.so" "$GO_MAIN_PATH"
    android_check_alignment "$ndk_bin/llvm-readelf" "$work/lib/$abi/libdens.so"
  done

  # The resources and the manifest, through aapt2, with the development
  # build's name over the release's. The code names no resource, so there's
  # no R class to make.
  mkdir -p "$work/overlay/values"
  printf '<resources><string name="name">Dens (development)</string></resources>\n' >"$work/overlay/values/name.xml"
  run_step "Compiled the app's resources" "Compiling the app's resources failed" \
    "$build_tools/aapt2" compile --dir "$ANDROID_SOURCE/res" -o "$work/res.zip"
  run_step "Compiled the development build's name" "Compiling the development build's name failed" \
    "$build_tools/aapt2" compile "$work/overlay/values/name.xml" -o "$work"
  # Each development build updates the one before: its version code counts
  # the minutes since 2026 began.
  local code=$(( ($(date -u +%s) - 1767225600) / 60 ))
  run_step "Linked the manifest, version $VERSION ($code)" "Linking the manifest failed" \
    "$build_tools/aapt2" link -o "$work/base.apk" -I "$platform/android.jar" \
      --manifest "$ANDROID_SOURCE/AndroidManifest.xml" \
      --min-sdk-version "$ANDROID_MIN_SDK" --target-sdk-version "$ANDROID_TARGET_SDK" \
      --version-code "$code" --version-name "$VERSION" \
      --rename-manifest-package "$ANDROID_DEV_PACKAGE" --debug-mode \
      "$work/res.zip" -R "$work/values_name.arsc.flat"

  # The shell's Kotlin, then its dex with Kotlin's standard library.
  run_step "Compiled the shell" "Compiling the shell failed" \
    "$tools/kotlinc/bin/kotlinc" "$ANDROID_SOURCE/src" -cp "$platform/android.jar" -jvm-target 17 -no-reflect \
      -d "$work/classes.jar"
  run_step "Made the shell's dex" "Making the shell's dex failed" \
    "$build_tools/d8" --release --min-api "$ANDROID_MIN_SDK" --lib "$platform/android.jar" --output "$work/dex" \
      "$work/classes.jar" "$tools/kotlinc/lib/kotlin-stdlib.jar"
  # A build without Gradle has no Android Lint, whose NewApi check this
  # stands in for: a call newer than the oldest Android the app runs on
  # crashes there unless a check of the version guards it.
  run_step "Checked the shell's calls against Android $ANDROID_MIN_SDK" "The shell makes calls Android $ANDROID_MIN_SDK lacks" \
    python3 -I scripts/build/apicheck.py "$platform/data/api-versions.xml" "$work/classes.jar" "$ANDROID_MIN_SDK" \
      "$ANDROID_SOURCE/newer-apis.txt"

  android_dev_key
  for target in "${ANDROID_TARGETS[@]}"; do
    read -r goarch abi _ <<<"$target"
    cp "$work/base.apk" "$work/$abi.apk"
    android_add "$work/$abi.apk" "$work/dex" classes.dex
    android_add "$work/$abi.apk" "$work" "lib/$abi/libdens.so"
    run_step "Aligned the $abi APK" "Aligning the $abi APK failed" \
      "$build_tools/zipalign" -f -p 4 "$work/$abi.apk" "$work/$abi-aligned.apk"
    run_step "Signed $OUT_DIR/android-$goarch.apk" "Signing the $abi APK failed" \
      "$build_tools/apksigner" sign --ks "$ANDROID_DEV_KEY" --ks-pass pass:dens-dev --ks-key-alias dens-dev --v4-signing-enabled false \
        --out "$OUT_DIR/android-$goarch.apk" "$work/$abi-aligned.apk"
  done
}
