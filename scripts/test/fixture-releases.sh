#!/usr/bin/env bash

# Builds the unsigned fixture releases the lifecycle harnesses install:
#   DEST/        v0.0.1-e2e: version pointer, installer, releases/<version>/
#   DEST/next/   v0.0.2-e2e, the release the harnesses update to
# TARGET is linux-amd64, linux-arm64 or windows-amd64, and picks the
# installer (install.sh or install.ps1). Install with APP_SKIP_VERIFY=true
# and APP_RELEASE_URL pointing at DEST (or DEST/next).
#
# Usage: scripts/test/fixture-releases.sh DEST TARGET   (DEST must not exist)

set -euo pipefail

[[ $# -eq 2 ]] || { echo "usage: $0 DEST TARGET" >&2; exit 2; }
[[ ! -e "$1" ]] || { echo "error: $1 already exists" >&2; exit 1; }
mkdir -p "$1"
DEST=$(cd "$1" && pwd)
TARGET=$2
case $TARGET in
  linux-amd64|linux-arm64) ASSET=$TARGET INSTALLER=install.sh ;;
  windows-amd64) ASSET=$TARGET.exe INSTALLER=install.ps1 ;;
  *) echo "error: unknown target $TARGET" >&2; exit 2 ;;
esac

cd "$(dirname "$0")/../.."
# shellcheck source=../build.sh
source scripts/build.sh
ensure_embed_placeholders
DEV_MODE=false
CERT_IDENTITY=test-identity
OIDC_ISSUER=test-issuer
# The installers always pass the release URL they came from, so the one
# baked into the binaries is never used; it only has to be valid.
RELEASE_URL=file:///release/

# build_fixture ROOT VERSION
build_fixture() {
  local root=$1 dir="$1/releases/$2"
  VERSION=$2
  mkdir -p "$dir"
  GOOS=${TARGET%-*} GOARCH=${TARGET#*-} CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags="$(make_ldflags "$DEV_MODE")" -o "$root/$ASSET" ./cmd
  gzip -c -n "$root/$ASSET" > "$dir/$ASSET.gz"
  rm "$root/$ASSET"
  printf '%s\n' "$VERSION" > "$dir/version"
  (cd "$dir" && sha256sum "$ASSET.gz" version > checksums.txt)
  printf '%s\n' "$VERSION" > "$root/version"
  render_installer "scripts/$INSTALLER" "$root/$INSTALLER"
  # dens update fetches the bundle even when APP_SKIP_VERIFY skips the check.
  : > "$root/$INSTALLER.cosign.bundle"
}

build_fixture "$DEST" v0.0.1-e2e
build_fixture "$DEST/next" v0.0.2-e2e
printf 'Fixture releases for %s in %s\n' "$TARGET" "$DEST"
