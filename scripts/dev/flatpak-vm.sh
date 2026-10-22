#!/usr/bin/env bash
# Makes Dens's Flatpak from this tree for a manual test in a VM (M9.3): a
# release build at VERSION, page and all, committed with scripts/flatpak.sh
# to a repository signed with a test key, with the links to install it
# from, staged in VMware's shared folder. In the VM, serve.sh serves them at
# http://localhost:8080/, as the release host will serve Dens's: open that
# page in the VM's browser and click the link. A later VERSION, committed
# on top, is the update the VM's Dens finds.
#
# Usage: scripts/dev/flatpak-vm.sh VERSION     as v1.2.0-vm.1, then v1.2.0-vm.2
#
# The packaging runs in an Incus container, dens-flatpak-vm (Debian 13). The
# key lives in out/flatpak-vm/keys too, so a container made again signs with
# it: a VM keeps trusting only the key it installed with. STAGE is the
# shared folder's directory for it.

set -euo pipefail
cd "$(dirname "$0")/../.."
[[ $# -eq 1 && $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { sed -n '10p' "$0" >&2; exit 2; }
VERSION_ARG=$1
OUT=$PWD/out/flatpak-vm
CONTAINER=${CONTAINER:-dens-flatpak-vm}
STAGE=${STAGE:-/mnt/c/Users/datac/dens-vm/flatpak}
APP=chat.dens.Dens

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
  in_container mkdir -p /root/vm/scripts
  incus file push -q scripts/flatpak.sh "$CONTAINER/root/vm/scripts/flatpak.sh"
  incus file push -q -r scripts/flatpak "$CONTAINER/root/vm/scripts/"
  in_container sh -c 'command -v flatpak >/dev/null && command -v appstreamcli >/dev/null' ||
    in_container bash /root/vm/scripts/flatpak.sh deps
  if [[ -f $OUT/keys/secret.asc ]] && ! in_container test -d /root/vm/gnupg; then
    in_container mkdir -m 0700 /root/vm/gnupg
    incus file push -q "$OUT/keys/secret.asc" "$CONTAINER/root/vm/secret.asc"
    in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --import /root/vm/secret.asc 2>/dev/null
  fi
  if ! in_container test -d /root/vm/gnupg; then
    in_container mkdir -m 0700 /root/vm/gnupg
    in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --passphrase '' \
      --quick-gen-key 'Dens VM test <vm@dens.chat>' ed25519 sign never 2>/dev/null
    mkdir -p "$OUT/keys"
    in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --export-secret-keys --armor >"$OUT/keys/secret.asc"
  fi
}

# binary builds this tree's release binary at VERSION, with its page.
binary() {
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
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="$(make_ldflags false)" -o "$OUT/dens" ./cmd
  )
}

# links writes the .flatpakref the VM installs from, and a page that links it.
links() {
  local key
  key=$(in_container env GNUPGHOME=/root/vm/gnupg gpg --batch --export | base64 -w0)
  cat >"$OUT/web/$APP.flatpakref" <<EOF
[Flatpak Ref]
Name=$APP
Branch=stable
Title=Dens (a test build)
Url=http://localhost:8080/repo/
SuggestRemoteName=dens-vm
Homepage=https://dens.chat
GPGKey=$key
RuntimeRepo=https://dl.flathub.org/repo/flathub.flatpakrepo
IsRuntime=false
EOF
  cat >"$OUT/web/index.html" <<EOF
<!doctype html><title>Dens test build</title>
<body style="font: 16px system-ui; margin: 2em">
<h1>Dens $VERSION_ARG, a test build</h1>
<p><a href="$APP.flatpakref">Install Dens</a></p>
</body>
EOF
}

commit() {
  incus file push -q --mode 0755 "$OUT/dens" "$CONTAINER/root/vm/dens"
  local key
  key=$(in_container env GNUPGHOME=/root/vm/gnupg gpg --list-keys --with-colons | awk -F: '/^fpr/ { print $10; exit }')
  in_container env GNUPGHOME=/root/vm/gnupg GPG_KEY="$key" \
    bash /root/vm/scripts/flatpak.sh commit /root/vm/dens "$VERSION_ARG" /root/vm/web/repo
  in_container sh -c 'gpgconf --kill gpg-agent 2>/dev/null || :'
  rm -rf "$OUT/web"
  incus file pull -r -q "$CONTAINER/root/vm/web" "$OUT/"
  links
}

# stage copies the web root to the shared folder: objects and deltas first,
# refs and summaries last, as a release uploads, and the VM's serve.sh.
stage() {
  mkdir -p "$STAGE/web"
  rsync -rt --exclude='summary*' --exclude='summaries/' --exclude='refs/' "$OUT/web/" "$STAGE/web/"
  rsync -rt --delete "$OUT/web/" "$STAGE/web/"
  cat >"$STAGE/serve.sh" <<'EOF'
#!/bin/sh
# Serves the test build's repository at http://localhost:8080/, as the
# release host will serve Dens's. Leave it running while you install and
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
binary
commit
stage
