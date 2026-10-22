#!/usr/bin/env bash
# Builds M9's Flatpak spike: the probe, three builds of the app around it,
# two repositories signed with different kinds of key, and the links the
# VMs install from, then stages them in VMware's shared folder.
#
# Usage: spikes/flatpak/build.sh              build v1, v2 and v3; publish v1
#        spikes/flatpak/build.sh publish VER  publish v1 again, or v2 or v3, as a release would
#        spikes/flatpak/build.sh stage        copy what's built to the shared folder again
#
# The packaging runs in an Incus container (dens-flatpak-spike, Debian 13)
# with only the flatpak, ostree and gnupg packages, gdk-pixbuf's SVG loader
# for the icon, and AppStream's compose tool: no flatpak-builder, no SDK and
# no runtime. Each build carries a stripped dens: v1 Dens's first
# release, stripped after the fact, and v2 and v3 this tree's, as a release
# builds it. STAGE is the shared folder's spike directory.

set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$PWD
OUT=$ROOT/out/flatpak-spike
CONTAINER=${CONTAINER:-dens-flatpak-spike}
STAGE=${STAGE:-/mnt/c/Users/datac/dens-vm/flatpak-spike}
RUNTIME=org.freedesktop.Platform/x86_64/26.08
SDK=org.freedesktop.Sdk/x86_64/26.08
APP=chat.dens.Spike

in_container() { incus exec "$CONTAINER" -- "$@"; }

container() {
  if ! incus info "$CONTAINER" >/dev/null 2>&1; then
    incus launch dens-e2e/debian-13 "$CONTAINER" -c security.nesting=true >/dev/null
    for _ in $(seq 60); do
      in_container getent hosts deb.debian.org >/dev/null 2>&1 && break
      sleep 1
    done
  fi
  incus info "$CONTAINER" | grep -q '^Status: RUNNING' || incus start "$CONTAINER"
  if ! in_container dpkg-query -W flatpak ostree gnupg dbus librsvg2-common appstream-compose >/dev/null 2>&1; then
    in_container sh -c 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends flatpak ostree gnupg dbus librsvg2-common appstream-compose >/dev/null'
  fi
  # The keys outlive the container: a VM that installed from one keeps
  # trusting only it.
  if [[ -f $OUT/keys/secret.asc ]] && ! in_container test -f /root/spike/keys/secret.asc; then
    in_container mkdir -p /root/spike/keys
    incus file push -q "$OUT/keys/secret.asc" "$CONTAINER/root/spike/keys/secret.asc"
  fi
  in_container flatpak --version
}

# binaries builds the probe for each version, and the two dens builds.
binaries() {
  mkdir -p "$OUT/bin"
  for v in v1 v2 v3; do
    (cd spikes && CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$v" -o "$OUT/bin/fprobe-$v" ./flatpak)
  done
  (
    # shellcheck source=scripts/build.sh
    source scripts/build.sh
    vendor_first_release linux-amd64
    gzip -dc "$VENDOR_FIRST_RELEASE/linux-amd64.gz" >"$OUT/bin/dens-v1.0.0"
    strip "$OUT/bin/dens-v1.0.0"
    ensure_embed_placeholders
    # make_ldflags reads both.
    # shellcheck disable=SC2034
    DEV_MODE=false VERSION=v1.1.0-spike
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="$(make_ldflags false)" -o "$OUT/bin/dens-head" ./cmd
  )
  ls -l "$OUT/bin"
}

# app VERSION DENS lays out a build directory: the metadata and the files,
# which build-finish completes in the container.
app() {
  local v=$1 dir=$OUT/builds/$1
  rm -rf "$dir"
  mkdir -p "$dir/files/bin" "$dir/files/share/applications" "$dir/files/share/metainfo" "$dir/files/share/icons/hicolor/scalable/apps"
  install -m 0755 "$OUT/bin/fprobe-$v" "$dir/files/bin/fprobe"
  install -m 0755 "$OUT/bin/$2" "$dir/files/bin/dens"
  install -m 0644 docs/static/favicon.svg "$dir/files/share/icons/hicolor/scalable/apps/$APP.svg"
  printf '[Application]\nname=%s\nruntime=%s\nsdk=%s\n' "$APP" "$RUNTIME" "$SDK" >"$dir/metadata"
  cat >"$dir/files/share/applications/$APP.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Dens Spike
Comment=Probes the Flatpak form of Dens
Exec=fprobe launch
Icon=$APP
Terminal=false
StartupNotify=false
Categories=Network;Chat;
EOF
  cat >"$dir/files/share/metainfo/$APP.metainfo.xml" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<component type="desktop-application">
  <id>$APP</id>
  <metadata_license>CC0-1.0</metadata_license>
  <project_license>MIT</project_license>
  <name>Dens Spike</name>
  <summary>Probes the Flatpak form of Dens</summary>
  <description><p>A throwaway build for the Flatpak spike of Dens's M9.</p></description>
  <launchable type="desktop-id">$APP.desktop</launchable>
  <url type="homepage">https://dens.chat</url>
  <developer id="chat.dens"><name>Dens</name></developer>
  <releases><release version="${v#v}" date="$(date +%F)"/></releases>
  <content_rating type="oars-1.1"/>
</component>
EOF
}

push_builds() {
  in_container mkdir -p /root/spike/builds /root/spike/dens
  for v in "$@"; do
    in_container rm -rf "/root/spike/builds/$v"
    incus file push -r -q "$OUT/builds/$v" "$CONTAINER/root/spike/builds/"
  done
  incus file push -q "spikes/flatpak/package.sh" "$CONTAINER/root/spike/package.sh"
  incus file push -q "$OUT/bin/dens-v1.0.0" "$OUT/bin/dens-head" "$CONTAINER/root/spike/dens/"
}

pull() {
  rm -rf "$OUT/web" "$OUT/keys"
  incus file pull -r -q "$CONTAINER/root/spike/web" "$CONTAINER/root/spike/keys" "$OUT/"
}

# stage copies the web root and the VM's scripts to the shared folder:
# objects and deltas first, refs and summaries last, as a release uploads.
stage() {
  mkdir -p "$STAGE/web" "$STAGE/vm" "$STAGE/results"
  rsync -rt --exclude='summary*' --exclude='summaries/' --exclude='refs/' "$OUT/web/" "$STAGE/web/"
  rsync -rt --delete "$OUT/web/" "$STAGE/web/"
  rsync -rt spikes/flatpak/vm/ "$STAGE/vm/"
  cp spikes/flatpak/README.md "$STAGE/README.md"
  echo "Staged in $STAGE"
}

case ${1:-} in
  "")
    container
    binaries
    app v1 dens-v1.0.0
    app v2 dens-head
    app v3 dens-head
    push_builds v1 v2 v3
    in_container bash /root/spike/package.sh init
    in_container bash /root/spike/package.sh deltas dens-v1.0.0 dens-head
    pull
    stage
    ;;
  publish)
    [[ ${2:-} =~ ^v[123]$ ]] || { echo "usage: $0 publish v1|v2|v3" >&2; exit 2; }
    container
    # The build published carries the probe as it is now; v1 publishes
    # again on top of itself, as a fix would.
    binaries
    dens="dens-head"
    [[ $2 == v1 ]] && dens="dens-v1.0.0"
    app "$2" "$dens"
    push_builds "$2"
    in_container bash /root/spike/package.sh publish "$2"
    pull
    stage
    ;;
  stage)
    stage
    ;;
  *)
    sed -n '5,8p' "$0" >&2
    exit 2
    ;;
esac
