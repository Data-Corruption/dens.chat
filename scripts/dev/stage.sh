#!/usr/bin/env bash
# Stages a manual test's builds: release builds of this tree at VERSION, a
# prerelease of the coming version, page and all, as a release directory
# at DEST laid out as the release host's is, with its installers; and,
# with android among the targets, the Android app's development APK
# beside it, which phone.sh installs from out/. The binaries carry no signing
# identity, as the harnesses' fixtures don't, so they install, and update,
# with --skip-verify. A prerelease sorts before its release, so the
# release updates over it once published. Staging again into DEST, at a
# later prerelease, is the update the test machines find.
#
# Usage: scripts/dev/stage.sh VERSION DEST [TARGET...]
#   VERSION is vX.Y.Z-PRERELEASE, X.Y.Z past the newest release's tag,
#   and a later staging's PRERELEASE sorts after the one before. TARGETs
#   are linux-amd64, linux-arm64, windows-amd64, windows-arm64 and
#   android, linux-amd64, windows-amd64 and android if none are given.
#
# An install from DEST keeps DEST as its release URL, so dens update looks
# there until the guide's installer runs again. Building runs
# scripts/build.sh's helpers, but not build.sh, so out/ stays as it is;
# android runs build.sh --android, which writes out/android-*.apk.

set -euo pipefail
cd "$(dirname "$0")/../.."
usage() { awk '/^# Usage:/ { on = 1 } on && !/^# / { exit } on' "$0" >&2; exit 2; }
[[ $# -ge 2 && $1 =~ ^v([0-9]+\.[0-9]+\.[0-9]+)-[0-9A-Za-z.-]+$ ]] || usage
VERSION_ARG=$1
core=v${BASH_REMATCH[1]}
dest=$2
shift 2
wanted=("$@")
[[ ${#wanted[@]} -gt 0 ]] || wanted=(linux-amd64 windows-amd64 android)
for target in "${wanted[@]}"; do
  [[ $target =~ ^(linux-amd64|linux-arm64|windows-amd64|windows-arm64|android)$ ]] || usage
done

# One whose version sorts before a release would install below it, and
# the installers refuse to put an older version over a newer one.
newest=$(git tag --list 'v[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || :)
[[ -n $newest ]] || { echo "error: no release tags: git fetch --tags" >&2; exit 1; }
if [[ $core == "$newest" || $(printf '%s\n' "$core" "$newest" | sort -V | tail -n 1) != "$core" ]]; then
  echo "error: $VERSION_ARG isn't a prerelease past $newest" >&2
  exit 1
fi
mkdir -p "$dest"
DEST=$(cd "$dest" && pwd)
[[ ! -e $DEST/releases/$VERSION_ARG ]] || { echo "error: $DEST/releases/$VERSION_ARG exists" >&2; exit 1; }

for target in "${wanted[@]}"; do
  if [[ $target == android ]]; then
    ./scripts/build.sh --android
    cp out/android-arm64.apk "$DEST/android-arm64.apk"
  fi
done

(
  # shellcheck source=../build.sh
  source scripts/build.sh
  # make_ldflags and render_installer read these.
  # shellcheck disable=SC2034
  DEV_MODE=false PAGE_REQUIRED=true CERT_IDENTITY="" VERSION=$VERSION_ARG
  # The installers pass the release URL they came from, so the one baked
  # into the binaries is never used; it only has to be valid.
  RELEASE_URL=file:///release/
  validate_app_name
  detect_host_arch
  validate_pins
  dep_check
  frontend_build
  frontend_hash_assets
  dir=$DEST/releases/$VERSION
  mkdir -p "$dir"
  assets=()
  for target in "${wanted[@]}"; do
    [[ $target != android ]] || continue
    asset=$target
    [[ $target != windows-* ]] || asset+=.exe
    GOOS=${target%-*} GOARCH=${target#*-} CGO_ENABLED=0 go build -trimpath -buildvcs=false \
      -ldflags="$(make_ldflags false)" -o "$dir/$asset" ./cmd
    gzip -n "$dir/$asset"
    assets+=("$asset.gz")
  done
  printf '%s\n' "$VERSION" >"$dir/version"
  (cd "$dir" && sha256sum "${assets[@]}" version >checksums.txt)
  render_installer scripts/install.sh "$DEST/install.sh"
  render_installer scripts/install.ps1 "$DEST/install.ps1"
  printf '%s\n' "$VERSION" >"$DEST/version"
)

url=file://$DEST/
printf '\nStaged %s in %s. Install it with sudo, or from an elevated PowerShell:\n' "$VERSION_ARG" "$DEST"
printf '  sudo sh %s/install.sh --release-url %s --skip-verify\n' "$DEST" "$url"
if command -v wslpath >/dev/null && [[ $DEST == /mnt/* ]]; then
  windows=$(wslpath -m "$DEST")
  printf '  powershell -ExecutionPolicy Bypass -File %s/install.ps1 -ReleaseUrl file:///%s/ -SkipVerify\n' "$windows" "$windows"
fi
if [[ " ${wanted[*]} " == *" android "* ]]; then
  printf 'and the app on a phone: scripts/test/android/phone.sh SERIAL install arm64\n'
fi
