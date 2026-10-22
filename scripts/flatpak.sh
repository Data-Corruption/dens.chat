#!/usr/bin/env bash
# Makes Dens's Flatpak (M9.3) from a release's binary, and commits it to a
# repository: for the release pipeline, and for the Flatpak e2e. It runs
# where flatpak, ostree, gnupg, AppStream's compose tool and gdk-pixbuf's
# SVG loader are, as a Debian 13 container with those packages does, and
# needs no flatpak-builder, SDK or runtime: a written metadata names the
# runtime by its branch, whose newest commit clients take; AppStream's tool
# composes the data software centers list Dens by, its license and icon
# included; and flatpak itself finishes the build and exports it.
#
# Usage:
#   scripts/flatpak.sh deps                         as root on Debian: install what it needs
#   scripts/flatpak.sh commit BINARY VERSION REPO   commit BINARY, a release's dens, as VERSION
#
# commit makes REPO where there's none, signs with the GPG key GPG_KEY names,
# from GNUPGHOME, writes the static delta from the build before, and prunes
# all but the last few builds. ARCH, x86_64 or aarch64, is the binary's: the
# machine's unless set.

set -euo pipefail

APP=chat.dens.Dens
BRANCH=stable
# The freedesktop runtime, which a static binary uses nothing of, but every
# Flatpak names.
RUNTIME_BRANCH=26.08
FILES=$(cd "$(dirname "$0")/flatpak" && pwd)

deps() {
  apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
    flatpak ostree gnupg librsvg2-common appstream-compose >/dev/null
}

commit() {
  local binary=$1 version=$2 repo=$3 arch=${ARCH:-$(uname -m)}
  [[ -n ${GPG_KEY:-} ]] || { echo "error: GPG_KEY names no signing key" >&2; exit 2; }
  case $arch in
    x86_64|aarch64) ;;
    *) echo "error: unknown architecture $arch" >&2; exit 2 ;;
  esac
  local build sign=(--gpg-sign="$GPG_KEY")
  [[ -n ${GNUPGHOME:-} ]] && sign+=(--gpg-homedir="$GNUPGHOME")
  # build-export makes the repository, but not the directory it's in.
  mkdir -p "$(dirname "$repo")"
  build=$(mktemp -d)
  # shellcheck disable=SC2064 # the build directory is this call's
  trap "rm -rf '$build'" RETURN
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
  flatpak build-export --arch="$arch" "${sign[@]}" --subject="Dens $version" "$repo" "$build" "$BRANCH" >/dev/null
  flatpak build-update-repo "${sign[@]}" --title=Dens --default-branch="$BRANCH" \
    --generate-static-deltas --prune --prune-depth=3 "$repo" >/dev/null
  printf 'Committed Dens %s for %s: %s\n' "$version" "$arch" \
    "$(ostree rev-parse --repo="$repo" "app/$APP/$arch/$BRANCH" | cut -c1-12)"
}

case ${1:-} in
  deps)
    [[ $# -eq 1 ]] || { sed -n '11,13p' "$0" >&2; exit 2; }
    deps
    ;;
  commit)
    [[ $# -eq 4 ]] || { sed -n '11,13p' "$0" >&2; exit 2; }
    commit "$2" "$3" "$4"
    ;;
  *)
    sed -n '11,13p' "$0" >&2
    exit 2
    ;;
esac
