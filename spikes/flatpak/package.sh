#!/usr/bin/env bash
# Runs in the spike's container, as root, with only the flatpak, ostree and
# gnupg packages, gdk-pixbuf's SVG loader, which build-export checks the icon
# with, and AppStream's compose tool: makes the app's commits from the build directories
# build.sh pushed, signs them into two repositories, one per kind of key,
# and writes what the VMs install from. Everything lives in /root/spike.
#
# Usage: package.sh init           keys, every build finished, v1 published
#        package.sh publish VERSION add that build to both repositories
#        package.sh deltas         static deltas between the pushed dens builds

set -euo pipefail
cd /root/spike
export GNUPGHOME=/root/spike/gnupg
APP=chat.dens.Spike
REF=app/$APP/x86_64/stable
BASE_URL=http://localhost:8080

key_id() { gpg --list-keys --with-colons "$1" | awk -F: '/^fpr/ { print $10; exit }'; }

# keys makes the two signing keys once, and keeps them in keys/secret.asc,
# which build.sh copies out and back should the container go.
keys() {
  if [[ ! -d $GNUPGHOME ]]; then
    mkdir -m 0700 "$GNUPGHOME"
    if [[ -f keys/secret.asc ]]; then
      gpg --batch --import keys/secret.asc 2>/dev/null
    else
      gpg --batch --passphrase '' --quick-gen-key 'Dens spike Ed25519 <spike@dens.chat>' ed25519 sign never 2>/dev/null
      gpg --batch --passphrase '' --quick-gen-key 'Dens spike RSA <spike@dens.chat>' rsa4096 sign never 2>/dev/null
      mkdir -p keys
      gpg --batch --export-secret-keys --armor >keys/secret.asc
    fi
  fi
  ED=$(key_id 'Dens spike Ed25519')
  RSA=$(key_id 'Dens spike RSA')
}

# finish VERSION makes builds/VERSION a finished app: the metadata, its
# permissions and its exports, as flatpak-builder would.
finish() {
  local v=$1 dir=builds/$1
  [[ -d $dir/export ]] && return
  local sockets=(--socket=fallback-x11)
  # v3 asks for one permission more, which the update monitor must refuse
  # to install by itself.
  [[ $v == v3 ]] && sockets+=(--socket=wayland)
  # Software centers list an app, with its license and icon, from the
  # AppStream data composed from its metainfo, which flatpak-builder would
  # make; build-update-repo gathers it into the repository's catalog.
  appstreamcli compose --prefix=/ --origin="$APP" --components="$APP" --result-root="$dir/files" \
    --data-dir="$dir/files/share/app-info/xmls" --icons-dir="$dir/files/share/app-info/icons/flatpak" "$dir/files" >/dev/null
  flatpak build-finish "$dir" --command=fprobe --share=network "${sockets[@]}" >/dev/null
  echo "finished $v: $(grep -E '^(shared|sockets)=' "$dir/metadata" | tr '\n' ' ')"
}

# publish VERSION commits it to both repositories, each signed by its own
# key, and updates their summaries and deltas as a release would.
publish() {
  local v=$1 repo key
  finish "$v"
  for repo in repo-ed repo-rsa; do
    key=$ED
    [[ $repo == repo-rsa ]] && key=$RSA
    flatpak build-export --gpg-sign="$key" --gpg-homedir="$GNUPGHOME" --subject="Dens spike $v" "web/$repo" "builds/$v" stable >/dev/null
    flatpak build-update-repo --gpg-sign="$key" --gpg-homedir="$GNUPGHOME" --title='Dens spike' --default-branch=stable \
      --generate-static-deltas --prune --prune-depth=3 "web/$repo" >/dev/null
    echo "published $v to $repo: $(ostree rev-parse --repo="web/$repo" "$REF" | cut -c1-12)"
  done
  for repo in repo-ed repo-rsa; do
    echo "$repo deltas:"
    ostree static-delta list --repo="web/$repo" | sed 's/^/  /'
  done
  du -sh web/repo-ed/deltas 2>/dev/null | sed 's/^/  delta files: /'
}

# links writes the .flatpakref and .flatpakrepo files the VMs open, and a
# page that links them.
links() {
  local kind key name suffix
  for kind in ed rsa; do
    key=$ED name='Ed25519' suffix=''
    [[ $kind == rsa ]] && key=$RSA name='RSA-4096' suffix='-rsa'
    cat >"web/$APP$suffix.flatpakref" <<EOF
[Flatpak Ref]
Name=$APP
Branch=stable
Title=Dens spike ($name)
Url=$BASE_URL/repo-$kind/
SuggestRemoteName=dens-spike$suffix
Homepage=https://dens.chat
GPGKey=$(gpg --export "$key" | base64 -w0)
RuntimeRepo=https://dl.flathub.org/repo/flathub.flatpakrepo
IsRuntime=false
EOF
    cat >"web/dens-spike$suffix.flatpakrepo" <<EOF
[Flatpak Repo]
Title=Dens spike ($name)
Url=$BASE_URL/repo-$kind/
Homepage=https://dens.chat
DefaultBranch=stable
GPGKey=$(gpg --export "$key" | base64 -w0)
EOF
  done
  cat >web/index.html <<'EOF'
<!doctype html><title>Dens spike</title>
<body style="font: 16px system-ui; margin: 2em">
<h1>Dens spike</h1>
<p>Install one of these, then the other only after removing the first:</p>
<ul>
<li><a href="chat.dens.Spike.flatpakref">chat.dens.Spike.flatpakref</a>, signed with an Ed25519 key</li>
<li><a href="chat.dens.Spike-rsa.flatpakref">chat.dens.Spike-rsa.flatpakref</a>, signed with an RSA-4096 key</li>
</ul>
<p>The one-click form, which needs HTTPS, so here it only shows whether a software center takes the link:
<a href="flatpak+https://localhost:8080/chat.dens.Spike.flatpakref">flatpak+https://localhost:8080/chat.dens.Spike.flatpakref</a></p>
</body>
EOF
}

# deltas measures static deltas between the dens builds alone, with
# OSTree's defaults, which skip bsdiff when old and new pass 128 MB
# together, and with that limit raised.
deltas() {
  rm -rf deltas && mkdir deltas && ostree init --repo=deltas/repo --mode=archive-z2 >/dev/null
  local from=$1 to=$2 a b
  mkdir -p deltas/a deltas/b
  cp "dens/$from" deltas/a/dens
  cp "dens/$to" deltas/b/dens
  a=$(ostree commit --repo=deltas/repo --branch=from --tree=dir=deltas/a)
  b=$(ostree commit --repo=deltas/repo --branch=to --tree=dir=deltas/b)
  echo "dens $from: $(stat -c %s "dens/$from") bytes; $to: $(stat -c %s "dens/$to") bytes"
  echo "gzip of $to alone: $(gzip -c "dens/$to" | wc -c) bytes"
  for limit in 128 256; do
    ostree static-delta generate --repo=deltas/repo --from="$a" --to="$b" --max-bsdiff-size="$limit" >/dev/null
    echo "delta $from -> $to, bsdiff up to $limit MB: $(du -sb deltas/repo/deltas | cut -f1) bytes"
    ostree static-delta show --repo=deltas/repo "$a-$b" | grep -iE 'part|fallback|size' | sed 's/^/  /'
    rm -rf deltas/repo/deltas
  done
}

trap 'gpgconf --kill gpg-agent 2>/dev/null' EXIT

case ${1:-} in
  init)
    keys
    mkdir -p web
    for v in builds/*; do finish "$(basename "$v")"; done
    publish v1
    links
    echo "keys: Ed25519 $ED, RSA $RSA"
    ;;
  publish)
    keys
    publish "$2"
    ;;
  deltas)
    deltas "$2" "$3"
    ;;
  *)
    echo "usage: $0 init | publish VERSION | deltas FROM TO" >&2
    exit 2
    ;;
esac
