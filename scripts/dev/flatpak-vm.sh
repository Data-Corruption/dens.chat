#!/usr/bin/env bash
# Makes Dens's Flatpak from this tree for a manual test in a VM (M9.3,
# M9.4): release builds at VERSION, page and all, for x86_64 and aarch64,
# released to a release host in a directory and published there by the
# release pipeline's publisher (ci.sh --flatpak, through
# scripts/test/flatpak-release.sh), signed with a test key, and staged in
# VMware's shared folder with a page that links them. In the VM, serve.sh
# serves them at http://localhost:8080/, as the release host serves Dens's:
# open that page in the VM's browser and click the link. A later VERSION,
# published on top, is the update the VM's Dens finds.
#
# Usage: scripts/dev/flatpak-vm.sh VERSION     as v1.2.0-vm.1, then v1.2.0-vm.2
#
# The publishing runs in an Incus container, dens-flatpak-vm (Debian 13),
# which keeps the release host between runs. The key lives in
# out/flatpak-vm/keys too, so a container made again signs with it: a VM
# keeps trusting only the key it installed with. STAGE is the shared
# folder's directory for it.

set -euo pipefail
cd "$(dirname "$0")/../.."
[[ $# -eq 1 && $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { sed -n '12p' "$0" >&2; exit 2; }
VERSION_ARG=$1
OUT=$PWD/out/flatpak-vm
CONTAINER=${CONTAINER:-dens-flatpak-vm}
STAGE=${STAGE:-/mnt/c/Users/datac/dens-vm/flatpak}
APP=chat.dens.Dens
URL=http://localhost:8080/

in_container() { incus exec "$CONTAINER" -- "$@" </dev/null; }

container() {
  if ! incus info "$CONTAINER" >/dev/null 2>&1; then
    incus launch dens-e2e/debian-13 "$CONTAINER" -c security.nesting=true </dev/null >/dev/null
    for _ in $(seq 60); do
      in_container getent hosts deb.debian.org >/dev/null 2>&1 && break
      sleep 1
    done
  fi
  incus info "$CONTAINER" | grep -q '^Status: RUNNING' || incus start "$CONTAINER" </dev/null
  in_container mkdir -p /root/vm
  in_container rm -rf /root/vm/scripts
  incus file push -q -r scripts "$CONTAINER/root/vm/" </dev/null
  incus file push -q --mode 0755 "$(./scripts/vendor.sh rclone | sed -n 's/^rclone=//p')" \
    "$CONTAINER/usr/local/bin/rclone" </dev/null
  in_container sh -c 'command -v flatpak >/dev/null && command -v appstreamcli >/dev/null' ||
    in_container bash /root/vm/scripts/flatpak.sh deps
  if [[ -f $OUT/keys/secret.asc ]] && ! in_container test -d /root/vm/gnupg; then
    in_container mkdir -m 0700 /root/vm/gnupg
    incus file push -q "$OUT/keys/secret.asc" "$CONTAINER/root/vm/secret.asc" </dev/null
    in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --import /root/vm/secret.asc 2>/dev/null
  fi
  if ! in_container test -d /root/vm/gnupg; then
    in_container mkdir -m 0700 /root/vm/gnupg
    in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --passphrase '' \
      --quick-gen-key 'Dens VM test <vm@dens.chat>' ed25519 sign never 2>/dev/null
  fi
  mkdir -p "$OUT/keys"
  in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --export-secret-keys --armor >"$OUT/keys/secret.asc"
  in_container sh -c 'GNUPGHOME=/root/vm/gnupg gpg --batch --export-secret-keys --armor >/root/vm/key.secret.asc &&
    GNUPGHOME=/root/vm/gnupg gpg --batch --export --armor >/root/vm/key.asc &&
    GNUPGHOME=/root/vm/gnupg gpgconf --kill gpg-agent'
}

# binaries builds this tree's release binaries at VERSION, with their page.
binaries() {
  mkdir -p "$OUT"
  (
    # shellcheck source=scripts/build.sh
    source scripts/build.sh
    detect_host_arch
    frontend_build
    frontend_hash_assets
    # make_ldflags reads both.
    # shellcheck disable=SC2034
    DEV_MODE=false VERSION=$VERSION_ARG
    for arch in amd64 arm64; do
      CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -buildvcs=false -ldflags="$(make_ldflags false)" \
        -o "$OUT/dens-$arch" ./cmd
    done
  )
}

# publish releases VERSION to the container's release host and publishes
# its Flatpak, as the release pipeline does.
publish() {
  local arch
  for arch in amd64 arm64; do
    incus file push -q --mode 0755 "$OUT/dens-$arch" "$CONTAINER/root/vm/dens-$arch" </dev/null
  done
  in_container mkdir -p /root/vm/host
  in_container bash /root/vm/scripts/test/flatpak-release.sh release /root/vm/host "$VERSION_ARG" \
    /root/vm/dens-amd64 /root/vm/dens-arm64
  in_container bash /root/vm/scripts/test/flatpak-release.sh publish /root/vm/host "$URL" \
    /root/vm/key.secret.asc /root/vm/key.asc
  rm -rf "$OUT/web"
  mkdir -p "$OUT/web"
  incus file pull -r -q "$CONTAINER/root/vm/host/flatpak" "$OUT/web/" </dev/null
  cat >"$OUT/web/index.html" <<EOF
<!doctype html><title>Dens test build</title>
<body style="font: 16px system-ui; margin: 2em">
<h1>Dens $VERSION_ARG, a test build</h1>
<p><a href="flatpak/$APP.flatpakref">Install Dens</a></p>
</body>
EOF
}

# stage copies the web root to the shared folder: objects and deltas first,
# refs and summaries last, as the publisher uploads, and the VM's serve.sh.
stage() {
  mkdir -p "$STAGE/web"
  rsync -rt --exclude='summary*' --exclude='summaries/' --exclude='refs/' "$OUT/web/" "$STAGE/web/"
  rsync -rt --delete "$OUT/web/" "$STAGE/web/"
  cat >"$STAGE/serve.sh" <<'EOF'
#!/bin/sh
# Serves the test build's repository at http://localhost:8080/, as the
# release host serves Dens's. Leave it running while you install and
# update.
cd "$(dirname "$0")/web"
if command -v python3 >/dev/null 2>&1; then
    exec python3 -m http.server 8080
fi
exec nix-shell -p python3 --run 'python3 -m http.server 8080'
EOF
  echo "Staged Dens $VERSION_ARG in $STAGE"
}

container
binaries
publish
stage
