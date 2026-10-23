#!/usr/bin/env bash
# The Flatpak's publisher (M9.4), tested as root in a container from the
# Flatpak e2e's image: ./scripts/ci.sh --flatpak, through
# flatpak-release.sh, against a release host in a directory, served over
# HTTP, as members' Flatpaks read it. The builds are small scripts, which
# nothing runs. The member alice installs from the .flatpakref it writes
# and updates through its deltas, and through a change of key, and a
# client of no installation, made afresh for each look, reads the host as
# it stands.
#
# A wrapper on PATH counts the publisher's writes to the host, and fails
# the one FAIL_AT names, for a run cut short at each of them. No sandbox
# can start, as in the release job's container.

set -euo pipefail

SCRIPTS=$(cd "$(dirname "$0")/.." && pwd)
APP=chat.dens.Dens
HOST=/srv/publish
URL=http://127.0.0.1:8081/
KEYS=/root/publish-keys
WORK=/root/publish-work
SNAPSHOT=/root/publish-snapshot
export FAIL_AT=0 WRITES="$WORK/writes" WRITE_LOG="$WORK/write.log"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

as_alice() {
  runuser -u alice -- env XDG_RUNTIME_DIR="/run/user/$(id -u alice)" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$(id -u alice)/bus" "$@"
}

# key NAME [EXPIRY [PASSPHRASE]] makes a signing key, with its armored
# secret and public halves in KEYS.
key() {
  local home="$KEYS/$1"
  mkdir -m 0700 "$home"
  GNUPGHOME=$home gpg --batch --pinentry-mode loopback --passphrase "${3:-}" \
    --quick-gen-key "Dens $1 <$1@dens.test>" ed25519 sign "${2:-never}" 2>/dev/null
  GNUPGHOME=$home gpg --batch --passphrase "${3:-}" --pinentry-mode loopback --export-secret-keys --armor > "$KEYS/$1.secret"
  GNUPGHOME=$home gpg --batch --export --armor > "$KEYS/$1.asc"
  GNUPGHOME=$home gpgconf --kill gpg-agent
}

# release VERSION promotes VERSION, made of two small scripts.
release() {
  printf '#!/bin/sh\necho "Dens %s for x86_64"\n' "$1" > "$WORK/dens-x86_64"
  printf '#!/bin/sh\necho "Dens %s for aarch64"\n' "$1" > "$WORK/dens-aarch64"
  bash "$SCRIPTS/test/flatpak-release.sh" release "$HOST" "$1" "$WORK/dens-x86_64" "$WORK/dens-aarch64"
}

# publish [SECRET [PUBLIC]] runs the publisher, signing with SECRET, which
# PUBLIC stands in for the committed key of; its output goes to WORK/out.
publish() {
  printf '0\n' > "$WRITES"
  PATH="$WORK/wrap:$PATH" bash "$SCRIPTS/test/flatpak-release.sh" publish "$HOST" "$URL" \
    "${1:-$KEYS/dens.secret}" "${2:-$KEYS/dens.asc}" > "$WORK/out" 2>&1
}

# refused WHY [SECRET [PUBLIC]] runs the publisher, which must fail saying
# WHY and leave the host as it was.
refused() {
  local why=$1
  shift
  rm -rf "$WORK/before"
  cp -a "$HOST" "$WORK/before"
  if publish "$@"; then
    fail "the publisher went ahead where it should refuse ($why): $(cat "$WORK/out")"
  fi
  grep -qF "$why" "$WORK/out" || fail "the refusal doesn't say '$why': $(cat "$WORK/out")"
  diff -r "$WORK/before" "$HOST" >/dev/null || fail "a refusal ($why) changed the host"
}

# probe prints what a new client reads of Dens on the host, or fails.
probe() {
  local dir status=0
  dir=$(mktemp -d)
  FLATPAK_USER_DIR=$dir flatpak --user remote-add --gpg-import="$KEYS/dens.asc" dens "${URL}flatpak/repo/" &&
    FLATPAK_USER_DIR=$dir flatpak --user remote-info dens "$APP" || status=$?
  rm -rf "$dir"
  return "$status"
}

# serves VERSION checks a new client reads VERSION's build from the host.
serves() {
  probe 2>&1 | grep -qx " *Subject: Dens $1" || fail "the host doesn't serve $1: $(probe 2>&1 || :)"
}

# installed prints the version of alice's Dens.
installed() {
  as_alice flatpak --user info "$APP" | sed -n 's/^ *Subject: Dens //p'
}

mkdir -p "$WORK/wrap" "$HOST" "$KEYS"
# The release job's container, as Docker's are, can't make the namespaces
# Flatpak's sandbox needs, so nothing the publisher runs may need one:
# here, as there, bwrap fails.
mv /usr/bin/bwrap /usr/bin/bwrap.real
printf '#!/bin/sh\necho "bwrap: Creating new namespace failed: Operation not permitted" >&2\nexit 1\n' > /usr/bin/bwrap
chmod 0755 /usr/bin/bwrap
cat > "$WORK/wrap/rclone" <<'EOF'
#!/bin/sh
# rclone, counting the publisher's writes to the host and failing the one
# FAIL_AT names: a copy to it, or a delete in it.
case $1 in
  copy|copyto|sync|move|moveto) target=$3 ;;
  delete|deletefile|purge|rmdir|rmdirs) target=$2 ;;
  *) target= ;;
esac
case $target in
  /srv/publish|/srv/publish/*)
    n=$(($(cat "$WRITES") + 1))
    printf '%s\n' "$n" > "$WRITES"
    printf '%s %s\n' "$n" "$*" >> "$WRITE_LOG"
    if [ "$n" = "$FAIL_AT" ]; then
      echo "rclone: write $n fails, as the test asks" >&2
      exit 1
    fi
    ;;
esac
exec /usr/local/bin/rclone "$@"
EOF
chmod +x "$WORK/wrap/rclone"
systemd-run --unit=dens-publish-host python3 -m http.server 8081 --bind 127.0.0.1 --directory "$HOST" >/dev/null
for _ in $(seq 1 50); do
  curl -fsS -o /dev/null "$URL" 2>/dev/null && break
  sleep 0.1
done

echo ">> Keys: Dens's, another, one with a passphrase, and one that expires"
key dens
key other
key passphrase never 'a passphrase'
key expiring 1y

echo ">> A release before the Flatpak's first has nothing to publish"
release v1.1.0
publish || fail "the publisher failed on a release before the Flatpak: $(cat "$WORK/out")"
grep -q 'predates the Flatpak' "$WORK/out" || fail "nothing said why: $(cat "$WORK/out")"
[[ ! -e "$HOST/flatpak" ]] || fail "the publisher wrote a Flatpak of v1.1.0"

echo ">> Keys that aren't the committed one's secret half are refused"
release v1.2.0
: > "$KEYS/empty"
refused 'Flatpak signing not configured' "$KEYS/empty"
refused "that aren't revoked, and no other" "$KEYS/other.secret"
refused "can't sign without its passphrase" "$KEYS/passphrase.secret" "$KEYS/passphrase.asc"
refused 'never expires' "$KEYS/expiring.secret" "$KEYS/expiring.asc"
cat "$KEYS/dens.asc" "$KEYS/other.asc" > "$KEYS/two.asc"
refused "that aren't revoked, and no other" "$KEYS/dens.secret" "$KEYS/two.asc"

echo ">> The first publication makes the repository and its links"
: > "$WRITE_LOG"
publish || fail "the first publication failed: $(cat "$WORK/out")"
for link in "$APP.flatpakref:application/vnd.flatpak.ref" "dens.flatpakrepo:application/vnd.flatpak.repo"; do
  grep -qxF "Url=${URL}flatpak/repo/" "$HOST/flatpak/${link%%:*}" || fail "${link%%:*} names another repository"
  grep -qxF "GPGKey=$(gpg --dearmor < "$KEYS/dens.asc" | base64 -w0)" "$HOST/flatpak/${link%%:*}" ||
    fail "${link%%:*} carries another key"
  grep -F "flatpak/${link%%:*}" "$WRITE_LOG" | grep -qF "Content-Type: ${link#*:}" ||
    fail "${link%%:*} was uploaded untyped"
done
[[ "$(sed -n 1p "$HOST/.state/flatpak")" == v1.2.0 ]] || fail "the publication wasn't recorded"
serves v1.2.0
as_alice flatpak --user install -y --noninteractive --no-related "${URL}flatpak/$APP.flatpakref" >/dev/null
[[ "$(installed)" == v1.2.0 ]] || fail "alice installed $(installed)"

echo ">> Publishing the same version again writes nothing"
rm -rf "$WORK/before"
cp -a "$HOST" "$WORK/before"
publish || fail "publishing v1.2.0 again failed: $(cat "$WORK/out")"
grep -q 'already holds v1.2.0' "$WORK/out" || fail "it didn't say it holds v1.2.0: $(cat "$WORK/out")"
diff -r "$WORK/before" "$HOST" >/dev/null || fail "publishing v1.2.0 again changed the host"

echo ">> A release whose signature or checksums fail is refused"
release v1.2.1
cp "$HOST/releases/v1.2.1/checksums.txt.cosign.bundle" "$WORK/bundle"
printf '0%.0s' $(seq 64) > "$HOST/releases/v1.2.1/checksums.txt.cosign.bundle"
refused "doesn't verify"
cp "$WORK/bundle" "$HOST/releases/v1.2.1/checksums.txt.cosign.bundle"
cp "$HOST/releases/v1.2.1/linux-arm64.gz" "$WORK/arm64.gz"
printf 'swapped' | gzip -c > "$HOST/releases/v1.2.1/linux-arm64.gz"
refused 'checksum mismatch'
cp "$WORK/arm64.gz" "$HOST/releases/v1.2.1/linux-arm64.gz"

echo ">> A run cut short at any write leaves what clients read, and the next run finishes it"
rm -rf "$SNAPSHOT"
cp -a "$HOST" "$SNAPSHOT"
for n in $(seq 1 40); do
  rm -rf "$HOST"
  cp -a "$SNAPSHOT" "$HOST"
  : > "$WRITE_LOG"
  FAIL_AT=$n
  if publish; then
    break
  fi
  grep -q "write $n fails" "$WORK/out" || fail "the run cut short at write $n failed otherwise: $(cat "$WORK/out")"
  # A client reads the summary index and its signature together, which
  # only the moment between the two leaves apart.
  if ! grep "^$n " "$WRITE_LOG" | grep -q 'summary\.idx\.sig'; then
    probe > /dev/null 2>&1 || fail "cut short at write $n ($(grep "^$n " "$WRITE_LOG")), the host isn't readable"
  fi
  FAIL_AT=0
  publish || fail "the run after write $n failed: $(cat "$WORK/out")"
  serves v1.2.1
done
[[ $n -gt 5 && $n -lt 40 ]] || fail "the publication made $((n - 1)) writes"
serves v1.2.1
FAIL_AT=0

echo ">> Alice updates to v1.2.1 through the delta from v1.2.0"
journal_since=$(date '+%Y-%m-%d %H:%M:%S')
as_alice flatpak --user update -y --noninteractive --no-related "$APP" >/dev/null
[[ "$(installed)" == v1.2.1 ]] || fail "alice updated to $(installed)"
journalctl -u dens-publish-host --since "$journal_since" -q --no-pager | grep -q 'GET /flatpak/repo/deltas/.*/superblock' ||
  fail "alice's update used no delta"

echo ">> A host whose repository doesn't check out is refused"
release v1.2.2
object=$(find "$HOST/flatpak/repo/objects" -name '*.filez' | head -n 1)
cp "$object" "$WORK/object"
printf 'X' | dd of="$object" bs=1 seek=40 conv=notrunc 2>/dev/null
refused "doesn't check out"
cp "$WORK/object" "$object"
rm -rf "$SNAPSHOT"
cp -a "$HOST" "$SNAPSHOT"
# The host keeps no empty directories, which OSTree writes into.
mkdir -p "$HOST/flatpak/repo/refs/remotes" "$HOST/flatpak/repo/refs/mirrors" "$HOST/flatpak/repo/tmp"
GNUPGHOME="$KEYS/other" gpg --batch --export > "$WORK/other.gpg"
GNUPGHOME="$KEYS/other" GPG_KEY=$(GNUPGHOME="$KEYS/other" gpg --list-keys --with-colons | awk -F: '/^fpr/ { print $10; exit }') \
  TRUSTED_KEYS="$WORK/other.gpg" bash -c 'ARCH=x86_64 bash "$1/flatpak.sh" commit "$2" v1.2.2 "$3" && bash "$1/flatpak.sh" update "$3"' \
  sign "$SCRIPTS" "$WORK/dens-x86_64" "$HOST/flatpak/repo" >/dev/null
GNUPGHOME="$KEYS/other" gpgconf --kill gpg-agent
refused "doesn't check out"
rm -rf "$HOST"
cp -a "$SNAPSHOT" "$HOST"

echo ">> A repository newer than the promoted release is refused"
printf 'v1.2.0\n' > "$HOST/version"
refused 'refusing to move it back'
printf 'v1.2.2\n' > "$HOST/version"

echo ">> Each publication keeps four builds, and a delta from the one before"
for version in v1.2.2 v1.2.3 v1.2.4 v1.2.5; do
  release "$version"
  publish || fail "publishing $version failed: $(cat "$WORK/out")"
done
serves v1.2.5
mkdir -p "$HOST/flatpak/repo/refs/remotes" "$HOST/flatpak/repo/refs/mirrors"
for arch in x86_64 aarch64; do
  ref=app/$APP/$arch/stable
  builds=$(ostree log --repo="$HOST/flatpak/repo" "$ref" | grep -c '^commit ')
  [[ $builds -eq 4 ]] || fail "the repository keeps $builds builds for $arch"
  head=$(ostree rev-parse --repo="$HOST/flatpak/repo" "$ref")
  parent=$(ostree rev-parse --repo="$HOST/flatpak/repo" "$head^")
  ostree static-delta list --repo="$HOST/flatpak/repo" | grep -qx "$parent-$head" ||
    fail "no delta to $arch's build from the one before"
done
as_alice flatpak --user update -y --noninteractive --no-related "$APP" >/dev/null
[[ "$(installed)" == v1.2.5 ]] || fail "alice updated to $(installed)"

echo ">> A second key joins, every Flatpak takes it, and the first retires, revoked"
key next
cat "$KEYS/dens.asc" "$KEYS/next.asc" > "$KEYS/both.asc"
cat "$KEYS/dens.secret" "$KEYS/next.secret" > "$KEYS/both.secret"
release v1.2.6
publish "$KEYS/both.secret" "$KEYS/both.asc" || fail "publishing with both keys failed: $(cat "$WORK/out")"
mkdir -p "$HOST/flatpak/repo/refs/remotes" "$HOST/flatpak/repo/refs/mirrors"
for commit in $(ostree log --repo="$HOST/flatpak/repo" "app/$APP/x86_64/stable" | awk '/^commit / { print $2 }'); do
  [[ $(ostree show --repo="$HOST/flatpak/repo" "$commit" | grep -c 'Signature made') -eq 2 ]] ||
    fail "the build $commit doesn't carry both keys' signatures"
done
as_alice flatpak --user update -y --noninteractive --no-related "$APP" >/dev/null
[[ "$(installed)" == v1.2.6 ]] || fail "alice updated to $(installed)"
# trusted FINGERPRINT prints the validity alice's Flatpak gives a key, - or
# r for revoked, or nothing where it hasn't the key.
trusted() {
  gpg --no-default-keyring --keyring "/home/alice/.local/share/flatpak/repo/dens.trustedkeys.gpg" \
    --with-colons --list-keys 2>/dev/null |
    awk -F: -v key="$1" '$1 == "pub" { validity = $2 } $1 == "fpr" && $10 == key { print validity; exit }'
}
dens=$(GNUPGHOME="$KEYS/dens" gpg --list-keys --with-colons | awk -F: '/^fpr/ { print $10; exit }')
next=$(GNUPGHOME="$KEYS/next" gpg --list-keys --with-colons | awk -F: '/^fpr/ { print $10; exit }')
[[ -n "$(trusted "$next")" ]] || fail "alice's Flatpak didn't take the second key"
sed 's/^:-----BEGIN/-----BEGIN/' "$KEYS/dens/openpgp-revocs.d/$dens.rev" > "$KEYS/dens.rev"
revoking=$(mktemp -d)
GNUPGHOME=$revoking gpg --batch --quiet --import "$KEYS/dens.asc" "$KEYS/dens.rev" 2>/dev/null
GNUPGHOME=$revoking gpg --batch --export --armor "$dens" > "$KEYS/retired.asc"
cat "$KEYS/next.asc" >> "$KEYS/retired.asc"
release v1.2.7
refused "that aren't revoked, and no other" "$KEYS/both.secret" "$KEYS/retired.asc"
publish "$KEYS/next.secret" "$KEYS/retired.asc" || fail "publishing with the second key failed: $(cat "$WORK/out")"
as_alice flatpak --user update -y --noninteractive --no-related "$APP" >/dev/null
[[ "$(installed)" == v1.2.7 ]] || fail "alice updated to $(installed)"
[[ "$(trusted "$dens")" == r ]] || fail "alice's Flatpak didn't take the first key's revocation"

echo ">> A build that only the retired key signs, as a thief would publish, is refused"
mkdir -p "$HOST/flatpak/repo/refs/remotes" "$HOST/flatpak/repo/refs/mirrors" "$HOST/flatpak/repo/tmp"
GNUPGHOME="$KEYS/dens" gpg --batch --export > "$WORK/thief.gpg"
GNUPGHOME="$KEYS/dens" GPG_KEY=$dens TRUSTED_KEYS="$WORK/thief.gpg" \
  bash -c 'ARCH=x86_64 bash "$1/flatpak.sh" commit "$2" v6.6.6 "$3" && bash "$1/flatpak.sh" update "$3"' \
  sign "$SCRIPTS" "$WORK/dens-x86_64" "$HOST/flatpak/repo" >/dev/null
GNUPGHOME="$KEYS/dens" gpgconf --kill gpg-agent
as_alice flatpak --user update -y --noninteractive --no-related "$APP" >/dev/null 2>&1 || :
[[ "$(installed)" == v1.2.7 ]] || fail "alice took the retired key's build: $(installed)"

as_alice flatpak --user uninstall -y --noninteractive "$APP" >/dev/null
systemctl stop dens-publish-host
echo "The Flatpak's publisher: PASSED"
