#!/usr/bin/env bash
# Builds the Android e2e's fixture releases (M12.4): the app's release,
# chat.dens.Dens, for the emulator, as v99.0.1-e2e, which the e2e installs,
# and v99.0.2-e2e, which the app's updater updates it to, on a release host
# in DEST, which the e2e serves at FIXTURE_URL on the emulator's loopback.
# A Sigstore of the fixtures' own signs each release's checksums, as the
# release pipeline's cosign does, and the builds check releases against
# its trusted root in place of Sigstore's, as signed by the fixtures'
# identity. Beside v99.0.2-e2e's bundle is another, other.cosign.bundle,
# signed as another workflow, which the e2e serves first to see the
# updater refuse it. A key of the fixtures' own signs the APKs.
#
# Usage: scripts/test/android/releases.sh DEST   (DEST must not exist)

set -euo pipefail

[[ $# -eq 1 ]] || { echo "usage: $0 DEST" >&2; exit 2; }
[[ ! -e "$1" ]] || { echo "error: $1 already exists" >&2; exit 1; }
mkdir -p "$1/sigstore"
DEST=$(cd "$1" && pwd)

cd "$(dirname "$0")/../../.."
# shellcheck source=scripts/build.sh
source scripts/build.sh
detect_host_arch
# Where the e2e serves DEST: the emulator's loopback, which adb forwards to
# the container's.
FIXTURE_URL=http://127.0.0.1:18480/
FIXTURE_IDENTITY=https://example.test/dens/.github/workflows/release.yml@refs/heads/main
FIXTURE_ISSUER=https://issuer.example.test

# sigstore runs the fixtures' Sigstore, with what it's given in the
# environment.
sigstore() {
  env DENS_SIGSTORE_DIR="$DEST/sigstore" "$@" go test ./internal/platform/release/sigstoretest -run '^TestFixture$' -count=1 >/dev/null
}
sign() {
  sigstore DENS_SIGSTORE_SIGN="$1" DENS_SIGSTORE_IDENTITY="$2" DENS_SIGSTORE_ISSUER="$FIXTURE_ISSUER"
}
sigstore

android_tools
keytool -genkeypair -keystore "$DEST/key.p12" -storetype PKCS12 -storepass fixtures -alias "$ANDROID_KEY_ALIAS" \
  -keyalg EC -groupname secp256r1 -validity 365 -dname "CN=Dens fixtures" >/dev/null 2>&1
ANDROID_KEYSTORE=$(base64 -w0 "$DEST/key.p12")
ANDROID_KEYSTORE_PASSWORD=fixtures
export ANDROID_KEYSTORE ANDROID_KEYSTORE_PASSWORD
frontend_build
frontend_hash_assets

DEV_MODE=false
CERT_IDENTITY=$FIXTURE_IDENTITY
OIDC_ISSUER=$FIXTURE_ISSUER
RELEASE_URL=$FIXTURE_URL
SIGSTORE_ROOT=$(base64 -w0 "$DEST/sigstore/trusted_root.json")
for VERSION in v99.0.1-e2e v99.0.2-e2e; do
  dir="$DEST/releases/$VERSION"
  android_apks release "$dir" amd64
  printf '%s\n' "$VERSION" >"$dir/version"
  (cd "$dir" && sha256sum android-amd64.apk version >checksums.txt)
  sign "$dir/checksums.txt" "$FIXTURE_IDENTITY"
done
mkdir "$DEST/other"
cp "$dir/checksums.txt" "$DEST/other/"
sign "$DEST/other/checksums.txt" "${FIXTURE_IDENTITY/release.yml/other.yml}"
mv "$DEST/other/checksums.txt.cosign.bundle" "$dir/other.cosign.bundle"
rm -r "$DEST/other"
printf '%s\n' "$VERSION" >"$DEST/version"
printf 'Fixture releases of the Android app in %s, to serve at %s\n' "$DEST" "$FIXTURE_URL"
