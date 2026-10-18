#!/usr/bin/env bash

# Stages Dens's first release, as scripts/vendor.sh pins it, for the
# lifecycle harnesses to install with a den and update from (M9.1):
# DEST/first/ gets its binary for TARGET (linux-amd64, linux-arm64 or
# windows-amd64), named as the release names it, and its version file.
#
# Usage: scripts/test/first-release.sh DEST TARGET

set -euo pipefail

[[ $# -eq 2 ]] || { echo "usage: $0 DEST TARGET" >&2; exit 2; }
mkdir -p "$1"
DEST=$(cd "$1" && pwd)
TARGET=$2
case $TARGET in
  linux-amd64|linux-arm64) ASSET=$TARGET ;;
  windows-amd64) ASSET=$TARGET.exe ;;
  *) echo "error: unknown target $TARGET" >&2; exit 2 ;;
esac

cd "$(dirname "$0")/../.."
# shellcheck source=../build.sh
source scripts/build.sh
vendor_first_release "$TARGET"
mkdir -p "$DEST/first"
gzip -dc "$VENDOR_FIRST_RELEASE/$ASSET.gz" > "$DEST/first/$ASSET"
chmod 0755 "$DEST/first/$ASSET"
install -m 0644 "$VENDOR_FIRST_RELEASE/version" "$DEST/first/version"
printf 'Dens %s for %s in %s/first\n' "$DENS_FIRST_RELEASE" "$TARGET" "$DEST"
