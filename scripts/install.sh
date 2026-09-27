#!/bin/sh

# Dens installer for Linux.
#
# Downloads the current release, verifies its signature and checksum, and
# runs the release binary's install command as root. That command makes
# every change to the system; this script only fetches and verifies.
#
# Download this script and install.sh.cosign.bundle, verify them, then run:
#   sudo sh install.sh                        install, or update an installation
#   sudo sh install.sh --dry-run              show what would change
#   sudo sh install.sh --instance NAME --den  another instance, hosting a den
#   sudo sh install.sh --uninstall            remove an instance and its data
# Other options go to "dens install"; see "dens install --help".
#
# APP_RELEASE_URL installs from a byte-for-byte mirror of the release host;
# the signatures still verify. APP_SKIP_VERIFY=true skips the signature
# check (the checksum still applies), for testing unsigned releases only.

set -eu
umask 022

# Rendered by scripts/build.sh.
APP_NAME="<APP_NAME>"
RELEASE_URL="<RELEASE_URL>"
CERT_IDENTITY="<CERT_IDENTITY>"
OIDC_ISSUER="<OIDC_ISSUER>"
COSIGN_VERSION="<COSIGN_VERSION>"
COSIGN_SHA_LINUX_AMD64="<COSIGN_SHA_LINUX_AMD64>"
COSIGN_SHA_LINUX_ARM64="<COSIGN_SHA_LINUX_ARM64>"

fatal() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

[ "$(uname -s)" = Linux ] || fatal "this installer is for Linux"
[ "$(id -u)" -eq 0 ] || fatal "the installer changes the system; run it with sudo"

# --update is accepted for dens update and ignored: installing over an
# installation updates it. --uninstall hands off to the installed binary.
mode=install
for arg do
    shift
    case $arg in
        --update) ;;
        --uninstall) mode=uninstall ;;
        *) set -- "$@" "$arg" ;;
    esac
done

if [ "$mode" = uninstall ]; then
    installed=/usr/local/bin/$APP_NAME
    [ -x "$installed" ] || fatal "$APP_NAME isn't installed; nothing to uninstall"
    exec "$installed" uninstall "$@"
fi

RELEASE_URL=${APP_RELEASE_URL:-$RELEASE_URL}
RELEASE_URL=${RELEASE_URL%/}/
case $RELEASE_URL in
    https://*|file:///*) ;;
    *) fatal "release URL must use https: $RELEASE_URL" ;;
esac
SKIP_VERIFY=${APP_SKIP_VERIFY:-false}

case $(uname -m) in
    x86_64|amd64) arch=amd64 cosign_sha=$COSIGN_SHA_LINUX_AMD64 ;;
    aarch64|arm64) arch=arm64 cosign_sha=$COSIGN_SHA_LINUX_ARM64 ;;
    *) fatal "unsupported architecture $(uname -m); Dens runs on amd64 and arm64" ;;
esac

for tool in curl gzip sha256sum awk mktemp; do
    command -v "$tool" >/dev/null 2>&1 || fatal "$tool is required"
done

temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

fetch() {
    curl --fail --silent --show-error --location --proto '=https,file' \
        --connect-timeout 10 --retry 3 --retry-delay 1 --max-time 300 "$@"
}

# Read the version pointer once and pin every other download to it.
version=$(fetch "${RELEASE_URL}version" | tr -d '\r\n')
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$' ||
    fatal "the release host returned an invalid version: $version"
release="${RELEASE_URL}releases/${version}/"
asset="linux-${arch}.gz"

printf 'Downloading %s %s ...\n' "$APP_NAME" "$version"
fetch -o "$temp/$asset" "$release$asset"
fetch -o "$temp/version" "${release}version"
fetch -o "$temp/checksums.txt" "${release}checksums.txt"

cosign=""
if [ "$SKIP_VERIFY" = true ] || [ "$SKIP_VERIFY" = 1 ]; then
    printf 'APP_SKIP_VERIFY is set: NOT verifying the release signature. Only for testing.\n' >&2
else
    printf 'Verifying the release signature ...\n'
    fetch -o "$temp/checksums.txt.cosign.bundle" "${release}checksums.txt.cosign.bundle"
    fetch -o "$temp/cosign" "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-${arch}"
    [ "$(sha256sum "$temp/cosign" | awk '{print $1}')" = "$cosign_sha" ] ||
        fatal "the downloaded cosign doesn't match its pinned checksum"
    chmod 0755 "$temp/cosign"
    "$temp/cosign" verify-blob \
        --bundle "$temp/checksums.txt.cosign.bundle" \
        --certificate-identity "$CERT_IDENTITY" \
        --certificate-oidc-issuer "$OIDC_ISSUER" \
        "$temp/checksums.txt" >/dev/null 2>&1 ||
        fatal "the release signature doesn't verify; not installing"
    cosign=$temp/cosign
fi

check() {
    expected=$(awk -v f="$1" '$2 == f {print $1; exit}' "$temp/checksums.txt")
    [ -n "$expected" ] || fatal "checksums.txt has no entry for $1"
    [ "$(sha256sum "$temp/$1" | awk '{print $1}')" = "$expected" ] ||
        fatal "$1 doesn't match its checksum"
}
check "$asset"
check version
[ "$(tr -d '\r\n' < "$temp/version")" = "$version" ] ||
    fatal "the signed release is not version $version"

gzip -dc "$temp/$asset" > "$temp/$APP_NAME"
chmod 0755 "$temp/$APP_NAME"

set -- install --release-url "$RELEASE_URL" "$@"
if [ -n "$cosign" ]; then
    set -- "$@" --cosign "$cosign"
fi
status=0
"$temp/$APP_NAME" "$@" || status=$?
exit "$status"
