# The Android app (M12): Dens built for Android with cgo through the NDK,
# and the APK around it, built without Gradle from the SDK's aapt2, d8,
# zipalign and apksigner, Kotlin's compiler and a JDK, as vendor.sh pins
# them. Sourced by ../build.sh.
#
# A development build is chat.dens.Dens.dev, labelled Dens (development),
# debuggable, and signed with a key of the developer's own, apart from the
# release's, so Play Protect's verdict on one never reaches the other. A
# release's is chat.dens.Dens, signed with the release key (M12.4).

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
# The release key's certificate, committed as the Flatpak's public key is:
# a release's APK must be signed by it, since every update must carry the
# signature of the key the app was installed with (release.md). The
# keystore that holds the key names it by this alias.
ANDROID_RELEASE_CERT="./android/release.pem"
ANDROID_KEY_ALIAS="dens"

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

# android_version_code VERSION prints a release's version code, vX.Y.Z as
# X*1000000 + Y*1000 + Z, which grows with every release, as Android needs
# of an update. A prerelease takes its release's.
android_version_code() {
  local core=${1#v} major minor patch
  core=${core%%[-+]*}
  IFS=. read -r major minor patch <<<"$core"
  if [[ ! "$major$minor$patch" =~ ^[0-9]+$ ]] || (( minor > 999 || patch > 999 )); then
    printf "🔴 %s has no Android version code: its minor and patch must be under 1000\n" "$1" >&2
    return 1
  fi
  printf '%d' $((10#$major * 1000000 + 10#$minor * 1000 + 10#$patch))
}

# android_tools sets the paths of what vendor.sh pins for the build.
android_tools() {
  vendor_android
  vendor_require_bins python3 unzip
  local tools
  tools=$(vendor_abs "$VENDOR_ANDROID")
  ANDROID_NDK_BIN="$tools/ndk/toolchains/llvm/prebuilt/linux-x86_64/bin"
  ANDROID_BUILD_TOOLS="$tools/sdk/build-tools/$ANDROID_BUILD_TOOLS_DIR"
  ANDROID_PLATFORM_DIR="$tools/sdk/platforms/$ANDROID_PLATFORM"
  ANDROID_KOTLINC="$tools/kotlinc"
  export JAVA_HOME="$tools/jdk"
  export PATH="$JAVA_HOME/bin:$PATH"
}

# android_check_signer APK CERT refuses an APK signed by anything but the
# one key CERT, a PEM certificate, holds: every signature the APK carries,
# in each of its schemes, must be by that key's certificate.
android_check_signer() {
  local want digests
  if [[ ! -f "$2" ]]; then
    printf "🔴 No %s: commit the release key's certificate there (release.md's The APK's key)\n" "$2" >&2
    return 1
  fi
  want=$(keytool -printcert -file "$2" | sed -n 's/^[[:space:]]*SHA256: //p' | tr -d ':' | tr 'A-F' 'a-f')
  digests=$("$ANDROID_BUILD_TOOLS/apksigner" verify --print-certs "$1" | sed -n 's/.*certificate SHA-256 digest: //p' | sort -u) || return 1
  if [[ -z "$want" || "$digests" != "$want" ]]; then
    printf "🔴 %s is signed by %s, not by the release key alone, as %s names it\n" "$1" "$(tr '\n' ' ' <<<"${digests:-nothing}")" "$2" >&2
    return 1
  fi
}

# android_build builds the development APKs: out/android-arm64.apk for
# phones, and out/android-amd64.apk for the emulator.
android_build() {
  android_tools
  frontend_build
  frontend_hash_assets
  android_apks dev "$OUT_DIR" arm64 amd64
}

# android_release DEST builds a release's APK for phones, as
# DEST/android-arm64.apk, around the release's Dens, from the page this
# build made (M12.4). It's signed with the keystore ANDROID_KEYSTORE holds
# in base64, which ANDROID_KEYSTORE_PASSWORD opens; the release
# pipeline's must hold the key the committed certificate names.
android_release() {
  android_tools
  android_apks release "$1" arm64
}

# android_apks KIND DEST GOARCH... builds the app's APK for each GOARCH, as
# DEST/android-GOARCH.apk. A development build (KIND dev) is
# chat.dens.Dens.dev, debuggable, and signed with the developer's own key;
# a release (KIND release) is chat.dens.Dens, at VERSION's version code,
# signed with the keystore in ANDROID_KEYSTORE. The page's assets must be
# built already.
android_apks() {
  local kind=$1 dest=$2
  shift 2
  local platform="$ANDROID_PLATFORM_DIR"
  # Its own directory: out/android holds what phone.sh and bg.sh keep of
  # each device.
  local work="$OUT_DIR/android-build"
  rm -rf "$work"
  [[ "$kind" == dev ]] && rm -f "$OUT_DIR"/android-*.apk*
  mkdir -p "$work/dex" "$dest"

  # The build's own flags, stripped for a development build too: its APK
  # goes over wireless debugging, and nothing debugs Dens on a phone with
  # symbols. -checklinkname=0 lets Pion's interface listing (wlynxg/anet)
  # reach what Android's netlink rules leave it.
  local ldflags goarch target abi triple
  ldflags="$(make_ldflags "$DEV_MODE") -s -w -checklinkname=0"
  for goarch in "$@"; do
    for target in "${ANDROID_TARGETS[@]}"; do
      [[ "$target" == "$goarch "* ]] && read -r _ abi triple <<<"$target"
    done
    mkdir -p "$work/lib/$abi"
    # cgo, so names resolve through Android's own resolver, which keeps
    # its Private DNS: Go's finds no resolver there. The NDK's clang for
    # the oldest Android the app runs on.
    run_step "Built Dens for Android $goarch" "Building Dens for Android $goarch failed" \
      env GOOS=android GOARCH="$goarch" CGO_ENABLED=1 CC="$ANDROID_NDK_BIN/$triple$ANDROID_MIN_SDK-clang" \
      go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$work/lib/$abi/libdens.so" "$GO_MAIN_PATH"
    android_check_alignment "$ANDROID_NDK_BIN/llvm-readelf" "$work/lib/$abi/libdens.so"
  done

  # The resources and the manifest, through aapt2, with a development
  # build's name over the release's. The code names no resource, so there's
  # no R class to make.
  run_step "Compiled the app's resources" "Compiling the app's resources failed" \
    "$ANDROID_BUILD_TOOLS/aapt2" compile --dir "$ANDROID_SOURCE/res" -o "$work/res.zip"
  local code link=()
  if [[ "$kind" == dev ]]; then
    mkdir -p "$work/overlay/values"
    printf '<resources><string name="name">Dens (development)</string></resources>\n' >"$work/overlay/values/name.xml"
    run_step "Compiled the development build's name" "Compiling the development build's name failed" \
      "$ANDROID_BUILD_TOOLS/aapt2" compile "$work/overlay/values/name.xml" -o "$work"
    # Each development build updates the one before: its version code
    # counts the minutes since 2026 began.
    code=$(( ($(date -u +%s) - 1767225600) / 60 ))
    link=(--rename-manifest-package "$ANDROID_DEV_PACKAGE" --debug-mode -R "$work/values_name.arsc.flat")
  else
    code=$(android_version_code "$VERSION")
  fi
  run_step "Linked the manifest, version $VERSION ($code)" "Linking the manifest failed" \
    "$ANDROID_BUILD_TOOLS/aapt2" link -o "$work/base.apk" -I "$platform/android.jar" \
      --manifest "$ANDROID_SOURCE/AndroidManifest.xml" \
      --min-sdk-version "$ANDROID_MIN_SDK" --target-sdk-version "$ANDROID_TARGET_SDK" \
      --version-code "$code" --version-name "$VERSION" \
      "$work/res.zip" "${link[@]}"

  # The shell's Kotlin, then its dex with Kotlin's standard library.
  run_step "Compiled the shell" "Compiling the shell failed" \
    "$ANDROID_KOTLINC/bin/kotlinc" "$ANDROID_SOURCE/src" -cp "$platform/android.jar" -jvm-target 17 -no-reflect \
      -d "$work/classes.jar"
  run_step "Made the shell's dex" "Making the shell's dex failed" \
    "$ANDROID_BUILD_TOOLS/d8" --release --min-api "$ANDROID_MIN_SDK" --lib "$platform/android.jar" --output "$work/dex" \
      "$work/classes.jar" "$ANDROID_KOTLINC/lib/kotlin-stdlib.jar"
  # A build without Gradle has no Android Lint, whose NewApi check this
  # stands in for: a call newer than the oldest Android the app runs on
  # crashes there unless a check of the version guards it.
  run_step "Checked the shell's calls against Android $ANDROID_MIN_SDK" "The shell makes calls Android $ANDROID_MIN_SDK lacks" \
    python3 -I scripts/build/apicheck.py "$platform/data/api-versions.xml" "$work/classes.jar" "$ANDROID_MIN_SDK" \
      "$ANDROID_SOURCE/newer-apis.txt"

  # The key: the developer's own, or the release's, which only its
  # keystore's password opens, never named on a command line, and which
  # goes as the build ends, however it ends.
  local sign=()
  if [[ "$kind" == dev ]]; then
    android_dev_key
    sign=(--ks "$ANDROID_DEV_KEY" --ks-pass pass:dens-dev --ks-key-alias dens-dev)
  else
    if [[ -z "${ANDROID_KEYSTORE:-}" || -z "${ANDROID_KEYSTORE_PASSWORD:-}" ]]; then
      printf "🔴 No release key: ANDROID_KEYSTORE and ANDROID_KEYSTORE_PASSWORD are empty (release.md's The APK's key)\n" >&2
      return 1
    fi
    ANDROID_KEY_DIR=$(mktemp -d)
    trap 'rm -rf "$ANDROID_KEY_DIR"' EXIT
    if ! base64 -d <<<"$ANDROID_KEYSTORE" >"$ANDROID_KEY_DIR/release.p12"; then
      printf "🔴 ANDROID_KEYSTORE isn't a keystore in base64\n" >&2
      return 1
    fi
    sign=(--ks "$ANDROID_KEY_DIR/release.p12" --ks-pass env:ANDROID_KEYSTORE_PASSWORD --ks-key-alias "$ANDROID_KEY_ALIAS")
  fi
  for goarch in "$@"; do
    for target in "${ANDROID_TARGETS[@]}"; do
      [[ "$target" == "$goarch "* ]] && read -r _ abi _ <<<"$target"
    done
    cp "$work/base.apk" "$work/$abi.apk"
    android_add "$work/$abi.apk" "$work/dex" classes.dex
    android_add "$work/$abi.apk" "$work" "lib/$abi/libdens.so"
    run_step "Aligned the $abi APK" "Aligning the $abi APK failed" \
      "$ANDROID_BUILD_TOOLS/zipalign" -f -p 4 "$work/$abi.apk" "$work/$abi-aligned.apk"
    run_step "Signed $dest/android-$goarch.apk" "Signing the $abi APK failed" \
      "$ANDROID_BUILD_TOOLS/apksigner" sign "${sign[@]}" --v4-signing-enabled false \
        --out "$dest/android-$goarch.apk" "$work/$abi-aligned.apk"
    # A release is signed by the release key alone.
    if [[ "$kind" == release && "$MODE" == ci ]]; then
      android_check_signer "$dest/android-$goarch.apk" "$ANDROID_RELEASE_CERT"
      printf "🟢 Checked %s is signed by the release key alone\n" "$dest/android-$goarch.apk"
    fi
  done
  if [[ -n "${ANDROID_KEY_DIR:-}" ]]; then
    rm -rf "$ANDROID_KEY_DIR"
    trap - EXIT
    ANDROID_KEY_DIR=""
  fi
}
