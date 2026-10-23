#!/usr/bin/env bash
# A release host in a directory, for the Flatpak's publisher (M9.4): the
# Flatpak e2e, the publisher's tests and the VMs' builds. release writes
# what a release leaves on the host, its checksums signed by a stand-in for
# cosign, as the release tests' are; publish runs ./scripts/ci.sh --flatpak
# against the directory, as rclone's local backend, with that stand-in.
# Runs where scripts/flatpak.sh does, with rclone on PATH, from a copy of
# the repository's scripts directory, this one's parent.
#
# Usage:
#   flatpak-release.sh release HOST VERSION AMD64 ARM64   promote VERSION, from these Linux binaries
#   flatpak-release.sh publish HOST URL KEY PUBLIC        publish HOST's Flatpak as served at URL,
#                                                         signed with KEY, an armored secret key,
#                                                         which PUBLIC stands in for the committed key of

set -euo pipefail

SCRIPTS=$(cd "$(dirname "$0")/.." && pwd)

release() {
  local host=$1 version=$2 amd64=$3 arm64=$4 dir="$1/releases/$2"
  mkdir -p "$dir"
  gzip -c -n "$amd64" > "$dir/linux-amd64.gz"
  gzip -c -n "$arm64" > "$dir/linux-arm64.gz"
  printf '%s\n' "$version" > "$dir/version"
  (cd "$dir" && sha256sum linux-amd64.gz linux-arm64.gz version > checksums.txt)
  # The stand-in's signature: the checksums' own checksum.
  sha256sum "$dir/checksums.txt" | awk '{ print $1 }' > "$dir/checksums.txt.cosign.bundle"
  printf '%s\n' "$version" > "$host/version"
}

publish() {
  local host=$1 url=$2 key=$3 public=$4 work status=0
  work=$(mktemp -d)
  mkdir "$work/bin"
  cat > "$work/bin/cosign" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ $1 == verify-blob ]] || exit 2
shift
bundle="" file=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --bundle) bundle=$2; shift 2 ;;
    --certificate-identity|--certificate-oidc-issuer) shift 2 ;;
    *) file=$1; shift ;;
  esac
done
[[ "$(tr -d '\r\n' < "$bundle")" == "$(sha256sum "$file" | awk '{ print $1 }')" ]]
EOF
  chmod +x "$work/bin/cosign"
  # shellcheck disable=SC2016 # expanded by the publisher's shell
  (
    cd "$work"
    PATH="$work/bin:$PATH" FLATPAK_GPG_KEY=$(cat "$key") GITHUB_REPOSITORY=example/dens \
      R2_ACCESS_KEY_ID=test R2_SECRET_ACCESS_KEY=test R2_ACCOUNT_ID=test R2_BUCKET=test \
      bash -c '
        set -euo pipefail
        scripts=$1 host=$2 url=$3 public=$4
        source "$scripts/ci.sh"
        vendor_cosign() { COSIGN_BIN=cosign; }
        configure_distribution() {
          PUBLISH_REMOTE=$host
          RCLONE_ARGS=()
          UPLOAD_ARGS=(--header-upload "$NO_CACHE" --ignore-times)
        }
        RELEASE_URL=$url
        FLATPAK_PUBLIC_KEY=$public
        ci_main --flatpak' publish "$SCRIPTS" "$host" "$url" "$public"
  ) || status=$?
  rm -rf "$work"
  return "$status"
}

case ${1:-} in
  release)
    [[ $# -eq 5 ]] || { sed -n '10,14p' "$0" >&2; exit 2; }
    release "$2" "$3" "$4" "$5"
    ;;
  publish)
    [[ $# -eq 5 ]] || { sed -n '10,14p' "$0" >&2; exit 2; }
    publish "$2" "$3" "$4" "$5"
    ;;
  *)
    sed -n '10,14p' "$0" >&2
    exit 2
    ;;
esac
