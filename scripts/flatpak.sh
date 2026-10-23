#!/usr/bin/env bash
# Makes Dens's Flatpak (M9.3) from a release's binary, and commits it to a
# repository: for the release pipeline (scripts/ci/flatpak.sh), and so for
# the Flatpak e2e and the VMs' builds. It runs where flatpak, ostree, gnupg,
# AppStream's compose tool and gdk-pixbuf's SVG loader are, as a Debian 13
# container with those packages does, and needs no flatpak-builder, SDK or
# runtime: a written metadata names the runtime by its branch, whose newest
# commit clients take; AppStream's tool composes the data software centers
# list Dens by, its license and icon included; and flatpak itself finishes
# the build and exports it.
#
# Usage:
#   scripts/flatpak.sh deps                         as root on Debian: install what it needs
#   scripts/flatpak.sh commit BINARY VERSION REPO   commit BINARY, a release's dens, as VERSION
#   scripts/flatpak.sh update REPO                  write REPO's deltas and summary, and prune it
#
# commit makes REPO where there's none, and signs the commit with each GPG
# key GPG_KEY names, from GNUPGHOME; ARCH, x86_64 or aarch64, is the
# binary's: the machine's unless set. update signs the same way, adding
# those keys' signatures to the builds it keeps, the last KEEP of each
# architecture (4 unless set), and has the summary name the public keys in
# TRUSTED_KEYS, a file, which every Flatpak takes at its next update.

set -euo pipefail

APP=chat.dens.Dens
BRANCH=stable
# The freedesktop runtime, which a static binary uses nothing of, but every
# Flatpak names.
RUNTIME_BRANCH=26.08
FILES=$(cd "$(dirname "$0")/flatpak" && pwd)
# OSTree compares a binary with the one before by bsdiff, which makes an
# update a fraction of the binary's size, only while the two are under its
# limit together: 128 MB unless raised, which two of Dens's builds will soon
# pass.
MAX_BSDIFF_MB=512

deps() {
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
    flatpak ostree gnupg librsvg2-common appstream-compose >/dev/null
}

# signing fills keys with the keys GPG_KEY names, homedir with the option
# that finds them, and sign with the options that sign with them.
signing() {
  read -r -a keys <<<"${GPG_KEY:-}"
  [[ ${#keys[@]} -gt 0 ]] || { echo "error: GPG_KEY names no signing key" >&2; exit 2; }
  local key
  homedir=()
  [[ -z ${GNUPGHOME:-} ]] || homedir=(--gpg-homedir="$GNUPGHOME")
  sign=("${homedir[@]}")
  for key in "${keys[@]}"; do
    sign+=(--gpg-sign="$key")
  done
}

commit() {
  local binary=$1 version=$2 repo=$3 arch=${ARCH:-$(uname -m)} keys homedir sign build
  signing
  case $arch in
    x86_64|aarch64) ;;
    *) echo "error: unknown architecture $arch" >&2; exit 2 ;;
  esac
  # build-export makes the repository, but not the directory it's in.
  mkdir -p "$(dirname "$repo")"
  build=$(mktemp -d)
  mkdir -p "$build/files/bin" "$build/files/share/applications" "$build/files/share/metainfo" \
    "$build/files/share/icons/hicolor/scalable/apps"
  install -m 0755 "$binary" "$build/files/bin/dens"
  install -m 0644 "$FILES/$APP.desktop" "$build/files/share/applications/$APP.desktop"
  install -m 0644 "$FILES/$APP.svg" "$build/files/share/icons/hicolor/scalable/apps/$APP.svg"
  sed -e "s/@VERSION@/${version#v}/" -e "s/@DATE@/$(date -u +%F)/" "$FILES/$APP.metainfo.xml" \
    >"$build/files/share/metainfo/$APP.metainfo.xml"
  printf '[Application]\nname=%s\nruntime=org.freedesktop.Platform/%s/%s\nsdk=org.freedesktop.Sdk/%s/%s\n' \
    "$APP" "$arch" "$RUNTIME_BRANCH" "$arch" "$RUNTIME_BRANCH" >"$build/metadata"
  appstreamcli compose --prefix=/ --origin="$APP" --components="$APP" --result-root="$build/files" \
    --data-dir="$build/files/share/app-info/xmls" --icons-dir="$build/files/share/app-info/icons/flatpak" \
    "$build/files" >/dev/null
  # With Wayland granted, fallback-x11 gives the app X11 only in an X11
  # session, where push to talk needs it.
  flatpak build-finish "$build" --command=dens --share=network --socket=wayland --socket=fallback-x11 >/dev/null
  # The subject names the version, which the publisher reads back. The
  # icon's validator runs unsandboxed: the icon is this repository's, and
  # the release job's container, as Docker's are, can't make the
  # namespaces a sandbox needs.
  flatpak build-export --arch="$arch" "${sign[@]}" --no-update-summary --disable-sandbox \
    --subject="Dens $version" "$repo" "$build" "$BRANCH" >/dev/null
  rm -rf "$build"
  printf 'Committed Dens %s for %s: %s\n' "$version" "$arch" \
    "$(ostree rev-parse --repo="$repo" "app/$APP/$arch/$BRANCH" | cut -c1-12)"
}

update() {
  local repo=$1 keep=${KEEP:-4} keys homedir sign arch to from commit key
  signing
  [[ $keep =~ ^[1-9][0-9]*$ ]] || { echo "error: KEEP must be a count of builds" >&2; exit 2; }
  [[ -f ${TRUSTED_KEYS:-} ]] || { echo "error: TRUSTED_KEYS names no file of keys" >&2; exit 2; }
  # Every build kept carries every signing key's signature, which leaves a
  # key that retires, revoked or not, no build that only it signed.
  for arch in $(ostree refs --repo="$repo" "app/$APP" | sed 's|/.*||'); do
    for commit in $(ostree log --repo="$repo" "app/$APP/$arch/$BRANCH" | awk '/^commit / { print $2 }' | head -n "$keep"); do
      for key in "${keys[@]}"; do
        ostree show --repo="$repo" "$commit" | grep -q "key ID ${key: -16}\$" && continue
        ostree gpg-sign --repo="$repo" "${homedir[@]}" "$commit" "$key"
      done
    done
  done
  # The delta from each architecture's build before, written first with
  # bsdiff past OSTree's limit; build-update-repo keeps a delta that's
  # there, and writes the rest.
  for arch in $(ostree refs --repo="$repo" "app/$APP" | sed 's|/.*||'); do
    to=$(ostree rev-parse --repo="$repo" "app/$APP/$arch/$BRANCH")
    from=$(ostree rev-parse --repo="$repo" "$to^" 2>/dev/null) || continue
    ostree show --repo="$repo" "$from" >/dev/null 2>&1 || continue
    ostree static-delta list --repo="$repo" | grep -qx "$from-$to" && continue
    ostree static-delta generate --repo="$repo" --from="$from" --to="$to" \
      --max-bsdiff-size="$MAX_BSDIFF_MB" >/dev/null
  done
  flatpak build-update-repo "${sign[@]}" --gpg-import="$TRUSTED_KEYS" --title=Dens --default-branch="$BRANCH" \
    --generate-static-deltas --prune --prune-depth=$((keep - 1)) "$repo" >/dev/null
}

case ${1:-} in
  deps)
    [[ $# -eq 1 ]] || { sed -n '12,15p' "$0" >&2; exit 2; }
    deps
    ;;
  commit)
    [[ $# -eq 4 ]] || { sed -n '12,15p' "$0" >&2; exit 2; }
    commit "$2" "$3" "$4"
    ;;
  update)
    [[ $# -eq 2 ]] || { sed -n '12,15p' "$0" >&2; exit 2; }
    update "$2"
    ;;
  *)
    sed -n '12,15p' "$0" >&2
    exit 2
    ;;
esac
