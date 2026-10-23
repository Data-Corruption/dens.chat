# Dens's Flatpak repository on the release host (M9.4).
#
# This file is sourced by ../ci.sh, whose --flatpak runs publish_flatpak
# once a release has finished. The repository, under flatpak/repo/, holds
# each promoted release's Linux binaries, committed by ../flatpak.sh for
# x86_64 and aarch64 and signed with the Flatpak's GPG key; beside it, the
# links members install from. Every run starts from the host: it reads the
# promoted version, mirrors the live repository down, checking it against
# the committed keys, and publishes only what's missing, so a retry
# finishes what an earlier run began, and a run with nothing to do writes
# nothing.

FLATPAK_APP=chat.dens.Dens
FLATPAK_BRANCH=stable
FLATPAK_HOMEPAGE=https://dens.chat
# The first release whose binary runs as the Flatpak (M9.3); there's
# nothing to publish before it.
FLATPAK_FIRST_RELEASE=v1.2.0
# The keys every Flatpak trusts: one, or two while a key is being replaced,
# the old one revoked at the end (see docs/dev/release.md). FLATPAK_GPG_KEY
# holds the secret halves of those that aren't revoked, and each signs.
FLATPAK_PUBLIC_KEY="$CI_SCRIPT_DIR/flatpak/dens.asc"
# Each Flatpak architecture, with the release asset it's made from.
FLATPAK_ARCHES=(x86_64:linux-amd64.gz aarch64:linux-arm64.gz)
# The summaries, in the order they're replaced: each index before the
# summary that ends the list, so a summary that's current means the rest are.
FLATPAK_SUMMARIES=(summary.idx summary.idx.sig summary summary.sig)
# What the last finished publication left: its version, then the checksums
# of the summaries it uploaded. A run that finds both still true is done.
FLATPAK_RECORD=.state/flatpak

FLATPAK_WORK=""
FLATPAK_REPO=""
# The signing keys' fingerprints, and every committed key in one file.
FLATPAK_KEYS=""
FLATPAK_TRUSTED=""
FLATPAK_UPLOAD_ARGS=()

prepare_flatpak_context() {
  MODE="ci"
  configure_release_identity
  validate_app_name
  detect_host_arch
  validate_pins
  require_distribution_config
  configure_distribution
  local tool
  for tool in flatpak ostree gpg gpgconf appstreamcli base64 gzip; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      printf "error: '%s' is required to publish the Flatpak; scripts/flatpak.sh deps installs it\n" "$tool" >&2
      exit 1
    fi
  done
  clean_out_dir
  # Absolute, for the file: URLs OSTree pulls from.
  FLATPAK_WORK="$(pwd)/$OUT_DIR/flatpak"
  FLATPAK_REPO="$FLATPAK_WORK/repo"
  FLATPAK_TRUSTED="$FLATPAK_WORK/trusted.gpg"
  mkdir -p "$FLATPAK_WORK"
  # Content-addressed objects, the most of what's uploaded, never change, so
  # a checksum skips the ones the host already holds.
  FLATPAK_UPLOAD_ARGS=(--header-upload "$NO_CACHE" --checksum)
  vendor_cosign
}

forget_flatpak_keys() {
  [[ -n "${GNUPGHOME:-}" && -d "$GNUPGHOME" ]] || return 0
  gpgconf --kill gpg-agent >/dev/null 2>&1 || :
  rm -rf "$GNUPGHOME"
}

# committed_flatpak_keys prints each committed key as
# FINGERPRINT:VALIDITY:ALGORITHM:EXPIRY:CURVE, where a revoked key's
# validity is r, and refuses a file without one key that isn't revoked.
# Each that isn't is Ed25519, which every client checks, and never
# expires, since a key that expired would fail every Flatpak's check, the
# summary's that would bring them its successor included. It reads with
# GnuPG's home, which it changes nothing in.
committed_flatpak_keys() {
  local committed
  committed=$(gpg --batch --with-colons --import-options show-only --import "$FLATPAK_PUBLIC_KEY" 2>/dev/null |
    awk -F: '$1 == "pub" { key = $2 ":" $4 ":" $7 ":" $17; want = 1; next }
      want && $1 == "fpr" { print $10 ":" key; want = 0 }')
  if ! awk -F: '$2 != "r" { found = 1 } END { exit !found }' <<<"$committed"; then
    printf "error: %s holds no key that isn't revoked\n" "$FLATPAK_PUBLIC_KEY" >&2
    return 1
  fi
  if awk -F: '$2 != "r" && !($3 == 22 && $4 == "" && $5 == "ed25519") { bad = 1 } END { exit !bad }' <<<"$committed"; then
    printf "error: each key in %s that isn't revoked must be an Ed25519 key that never expires\n" "$FLATPAK_PUBLIC_KEY" >&2
    return 1
  fi
  printf '%s\n' "$committed"
}

# import_flatpak_keys imports FLATPAK_GPG_KEY into a GnuPG home of this
# run's own, and refuses anything but the secret halves of the committed
# keys that aren't revoked, whose copy here signs without a passphrase,
# which no job could type.
import_flatpak_keys() {
  if [[ -z "${FLATPAK_GPG_KEY:-}" ]]; then
    printf "🔴 Flatpak signing not configured: FLATPAK_GPG_KEY, the flatpak environment's secret, is empty\n" >&2
    exit 1
  fi
  GNUPGHOME=$(mktemp -d)
  export GNUPGHOME
  trap forget_flatpak_keys EXIT
  local committed signers held key
  committed=$(committed_flatpak_keys) || exit 1
  signers=$(awk -F: '$2 != "r" { print $1 }' <<<"$committed" | sort)
  if ! printf '%s\n' "$FLATPAK_GPG_KEY" | gpg --batch --quiet --import >/dev/null 2>&1; then
    printf "error: FLATPAK_GPG_KEY holds nothing GnuPG can import\n" >&2
    exit 1
  fi
  held=$(gpg --batch --with-colons --list-secret-keys 2>/dev/null |
    awk -F: '$1 == "sec" { want = 1; next } want && $1 == "fpr" { print $10; want = 0 }' | sort)
  if [[ "$held" != "$signers" ]]; then
    printf "error: FLATPAK_GPG_KEY must hold the secret keys of %s that aren't revoked, and no other: %s\n" \
      "$FLATPAK_PUBLIC_KEY" "$(paste -sd ' ' <<<"$signers")" >&2
    exit 1
  fi
  for key in $signers; do
    if ! printf 'dens\n' | gpg --batch --pinentry-mode error --local-user "$key" --detach-sign >/dev/null 2>&1; then
      printf "error: FLATPAK_GPG_KEY's %s can't sign without its passphrase; the environment's copy must have none\n" "$key" >&2
      exit 1
    fi
  done
  # Every committed key in one file, revocations and all: the links carry
  # it, and the summary names it, for every Flatpak to take.
  gpg --batch --quiet --import "$FLATPAK_PUBLIC_KEY" >/dev/null 2>&1
  # shellcheck disable=SC2046 # one fingerprint per word
  gpg --batch --export $(cut -d: -f1 <<<"$committed") > "$FLATPAK_TRUSTED"
  FLATPAK_KEYS=$(paste -sd ' ' <<<"$signers")
  printf "🟢 Signing with the Flatpak's keys: %s\n" "$FLATPAK_KEYS"
}

# mirror_flatpak_repository copies the live repository down and pulls its
# builds into a fresh local one, which checks every commit against the
# committed keys and every object against its checksum: a repository that
# fails is refused, never repaired. The host's summary only lists the
# builds; the publisher signs a new one from what it checked, which leaves
# out anything else the host holds. A host with no summary, before the
# first publication or after one cut short before its summary, which no
# client could have installed from, gets a new repository.
mirror_flatpak_repository() {
  local live="$FLATPAK_WORK/live" status listing entry ref refs=()
  if remote_file_exists flatpak/repo/summary; then
    :
  else
    status=$?
    [[ "$status" -eq "$REMOTE_ABSENT" ]] || exit "$status"
    printf "🟢 The release host has no Flatpak repository yet; making one\n"
    return
  fi
  run_step "Copied the live Flatpak repository down" "Failed to copy the live Flatpak repository down" \
    rclone copy "$PUBLISH_REMOTE/flatpak/repo" "$live" "${RCLONE_ARGS[@]}"
  ostree init --repo="$FLATPAK_REPO" --mode=archive-z2
  ostree remote add --repo="$FLATPAK_REPO" --set=gpg-verify=true --set=gpg-verify-summary=false \
    --gpg-import="$FLATPAK_TRUSTED" live "file://$live" >/dev/null
  if ! listing=$(ostree remote refs --repo="$FLATPAK_REPO" live 2>&1); then
    printf "error: the live Flatpak repository's summary can't be read:\n%s\n" "$listing" >&2
    exit 1
  fi
  for entry in "${FLATPAK_ARCHES[@]}"; do
    ref="app/$FLATPAK_APP/${entry%%:*}/$FLATPAK_BRANCH"
    if grep -qxF "live:$ref" <<<"$listing"; then
      refs+=("$ref")
    fi
  done
  if [[ "${#refs[@]}" -gt 0 ]]; then
    # --untrusted checks each object's checksum, which a pull from a
    # local copy otherwise skips.
    run_step "Checked the live Flatpak repository against the Flatpak's key" \
      "The live Flatpak repository doesn't check out against the Flatpak's key; refusing to build on it" \
      ostree pull --repo="$FLATPAK_REPO" --mirror --untrusted --depth=-1 live "${refs[@]}"
  fi
  ostree remote delete --repo="$FLATPAK_REPO" live
  rm -rf "$live"
}

# flatpak_held ARCH prints the version of the build the repository holds
# for ARCH, which its commit's subject names, or nothing where it has none.
flatpak_held() {
  local ref="app/$FLATPAK_APP/$1/$FLATPAK_BRANCH" subject version
  ostree rev-parse --repo="$FLATPAK_REPO" "$ref" >/dev/null 2>&1 || return 0
  subject=$(ostree show --repo="$FLATPAK_REPO" "$ref" |
    awk 'NF == 0 { blank = 1; next } blank { sub(/^ +/, ""); print; exit }')
  version=${subject#Dens }
  if [[ "$subject" != "Dens $version" ]] || ! validate_version "$version" 2>/dev/null; then
    printf "error: the Flatpak repository's %s build is %q, which names no version\n" "$1" "$subject" >&2
    return 1
  fi
  printf '%s\n' "$version"
}

# flatpak_published VERSION reports whether the last finished publication
# was VERSION's, and the host still serves the summaries it left.
flatpak_published() {
  local dir="$FLATPAK_WORK/published" status file
  if remote_file_exists "$FLATPAK_RECORD"; then
    :
  else
    status=$?
    [[ "$status" -eq "$REMOTE_ABSENT" ]] && return 1
    exit "$status"
  fi
  mkdir -p "$dir"
  rclone copyto "$PUBLISH_REMOTE/$FLATPAK_RECORD" "$dir/record" "${RCLONE_ARGS[@]}" >/dev/null 2>&1 || return 1
  [[ "$(sed -n 1p "$dir/record")" == "$1" ]] || return 1
  for file in "${FLATPAK_SUMMARIES[@]}"; do
    rclone copyto "$PUBLISH_REMOTE/flatpak/repo/$file" "$dir/$file" "${RCLONE_ARGS[@]}" >/dev/null 2>&1 || return 1
  done
  (cd "$dir" && sed 1d record | sha256sum --check --strict --status)
}

# fetch_flatpak_release VERSION downloads the release's Linux binaries and
# checks them as an install does: cosign's signature over the checksums,
# with the release identity, then each file's checksum.
fetch_flatpak_release() {
  local version=$1 dir="$FLATPAK_WORK/release" entry file expected count
  local files=(checksums.txt checksums.txt.cosign.bundle version)
  for entry in "${FLATPAK_ARCHES[@]}"; do
    files+=("${entry#*:}")
  done
  for file in "${files[@]}"; do
    download_remote_file "releases/$version/$file" "$dir/$file"
  done
  if ! "$COSIGN_BIN" verify-blob \
      --bundle "$dir/checksums.txt.cosign.bundle" \
      --certificate-identity "$CERT_IDENTITY" \
      --certificate-oidc-issuer "$OIDC_ISSUER" \
      "$dir/checksums.txt" >/dev/null 2>&1; then
    printf "error: the signature over releases/%s/checksums.txt doesn't verify\n" "$version" >&2
    exit 1
  fi
  for file in "${files[@]:2}"; do
    count=$(awk -v file="$file" '$2 == file { count++ } END { print count + 0 }' "$dir/checksums.txt")
    if [[ "$count" -ne 1 ]]; then
      printf "error: releases/%s/checksums.txt has %d entries for %s\n" "$version" "$count" "$file" >&2
      exit 1
    fi
    expected=$(awk -v file="$file" '$2 == file { print $1 }' "$dir/checksums.txt")
    verify_sha256 "$dir/$file" "$expected" "releases/$version/$file" || exit 1
  done
  if [[ "$(tr -d '\r\n' < "$dir/version")" != "$version" ]]; then
    printf "error: releases/%s/version names another version\n" "$version" >&2
    exit 1
  fi
  for entry in "${FLATPAK_ARCHES[@]}"; do
    gzip -dc "$dir/${entry#*:}" > "$dir/dens-${entry%%:*}"
  done
  printf "🟢 Verified release %s's Linux binaries\n" "$version"
}

# check_flatpak_summary DIR checks the summary in DIR against the committed
# key, as a client does.
check_flatpak_summary() {
  local probe status=0
  probe=$(mktemp -d "$FLATPAK_WORK/probe.XXXXXX")
  ostree init --repo="$probe" --mode=archive-z2
  ostree remote add --repo="$probe" --set=gpg-verify=true --set=gpg-verify-summary=true \
    --gpg-import="$FLATPAK_TRUSTED" check "file://$1" >/dev/null
  ostree remote summary --repo="$probe" check >/dev/null 2>&1 || status=$?
  rm -rf "$probe"
  return "$status"
}

# upload_flatpak_repository uploads the local repository in an order a
# client can always read from: what no summary names yet first, then the
# refs, then the summaries, each index before its summary; deletions last,
# of what the new summaries no longer name. Only a moment between a summary
# and its signature, which a client retries past, reads as neither.
upload_flatpak_repository() {
  local remote="$PUBLISH_REMOTE/flatpak/repo" file
  local local_only=(--exclude '/.lock' --exclude '/tmp/**' --exclude '/state/**' --exclude '/extensions/**')
  run_step "Uploaded the Flatpak repository's objects and deltas" "Failed to upload the Flatpak repository's objects and deltas" \
    rclone copy "$FLATPAK_REPO" "$remote" "${local_only[@]}" --exclude '/summary*' --exclude '/summaries/**' \
      --exclude '/refs/**' --exclude '/config' "${FLATPAK_UPLOAD_ARGS[@]}" "${RCLONE_ARGS[@]}"
  run_step "Uploaded the Flatpak repository's summary parts" "Failed to upload the Flatpak repository's summary parts" \
    rclone copy "$FLATPAK_REPO/summaries" "$remote/summaries" "${FLATPAK_UPLOAD_ARGS[@]}" "${RCLONE_ARGS[@]}"
  run_step "Uploaded the Flatpak repository's refs" "Failed to upload the Flatpak repository's refs" \
    rclone copy "$FLATPAK_REPO" "$remote" --include '/refs/**' --include '/config' "${FLATPAK_UPLOAD_ARGS[@]}" "${RCLONE_ARGS[@]}"
  for file in "${FLATPAK_SUMMARIES[@]}"; do
    run_step "Uploaded flatpak/repo/$file" "Failed to upload flatpak/repo/$file" \
      rclone copyto "$FLATPAK_REPO/$file" "$remote/$file" "${FLATPAK_UPLOAD_ARGS[@]}" "${RCLONE_ARGS[@]}"
  done
  run_step "Deleted what the Flatpak repository no longer holds" "Failed to delete what the Flatpak repository no longer holds" \
    rclone sync "$FLATPAK_REPO" "$remote" "${local_only[@]}" "${FLATPAK_UPLOAD_ARGS[@]}" "${RCLONE_ARGS[@]}"
}

# check_flatpak_upload reads the summaries back from the host: the same
# bytes as the local repository's, and a summary that checks out.
check_flatpak_upload() {
  local dir="$FLATPAK_WORK/uploaded" file
  mkdir -p "$dir"
  for file in "${FLATPAK_SUMMARIES[@]}"; do
    if ! rclone copyto "$PUBLISH_REMOTE/flatpak/repo/$file" "$dir/$file" "${RCLONE_ARGS[@]}" >/dev/null 2>&1 ||
        ! cmp -s "$dir/$file" "$FLATPAK_REPO/$file"; then
      printf "error: flatpak/repo/%s on the release host isn't the one uploaded\n" "$file" >&2
      exit 1
    fi
  done
  if ! check_flatpak_summary "$dir"; then
    printf "error: the release host's Flatpak summary doesn't check out against the Flatpak's key\n" >&2
    exit 1
  fi
}

# write_flatpak_links writes the files members install from, which carry
# the committed keys: the app's .flatpakref, and the repository's
# .flatpakrepo, for adding it as a remote.
write_flatpak_links() {
  local dir="$FLATPAK_WORK/links" url="${RELEASE_URL}flatpak/repo/" key
  mkdir -p "$dir"
  key=$(base64 -w0 < "$FLATPAK_TRUSTED")
  cat > "$dir/$FLATPAK_APP.flatpakref" <<EOF
[Flatpak Ref]
Title=Dens
Name=$FLATPAK_APP
Branch=$FLATPAK_BRANCH
Url=$url
SuggestRemoteName=dens
Homepage=$FLATPAK_HOMEPAGE
IsRuntime=false
RuntimeRepo=https://dl.flathub.org/repo/flathub.flatpakrepo
GPGKey=$key
EOF
  cat > "$dir/dens.flatpakrepo" <<EOF
[Flatpak Repo]
Title=Dens
Url=$url
Homepage=$FLATPAK_HOMEPAGE
Comment=Dens's own repository
GPGKey=$key
EOF
}

# publish_flatpak_links uploads whichever link changed, typed so a browser
# hands it to the software center.
publish_flatpak_links() {
  local dir="$FLATPAK_WORK/links" link file type
  for link in "$FLATPAK_APP.flatpakref:application/vnd.flatpak.ref" "dens.flatpakrepo:application/vnd.flatpak.repo"; do
    file=${link%%:*}
    type=${link#*:}
    if rclone copyto "$PUBLISH_REMOTE/flatpak/$file" "$dir/$file.live" "${RCLONE_ARGS[@]}" >/dev/null 2>&1 &&
        cmp -s "$dir/$file.live" "$dir/$file"; then
      continue
    fi
    run_step "Uploaded flatpak/$file" "Failed to upload flatpak/$file" \
      rclone copyto "$dir/$file" "$PUBLISH_REMOTE/flatpak/$file" --header-upload "Content-Type: $type" \
        "${UPLOAD_ARGS[@]}" "${RCLONE_ARGS[@]}"
  done
}

# record_flatpak_publication VERSION records a finished publication, last.
record_flatpak_publication() {
  local record="$FLATPAK_WORK/record"
  {
    printf '%s\n' "$1"
    (cd "$FLATPAK_REPO" && sha256sum "${FLATPAK_SUMMARIES[@]}")
  } > "$record"
  upload_remote_file "$record" "$FLATPAK_RECORD"
}

publish_flatpak() {
  prepare_flatpak_context
  import_flatpak_keys
  local version status entry arch held needed=()
  if version=$(read_remote_version); then
    :
  else
    status=$?
    [[ "$status" -eq "$REMOTE_ABSENT" ]] && printf "error: no release is promoted to make the Flatpak from\n" >&2
    exit 1
  fi
  if [[ "$(semver_compare "$version" "$FLATPAK_FIRST_RELEASE")" == -1 ]]; then
    printf "🟢 %s predates the Flatpak, which starts at %s: nothing to publish\n" "$version" "$FLATPAK_FIRST_RELEASE"
    return
  fi

  mirror_flatpak_repository
  for entry in "${FLATPAK_ARCHES[@]}"; do
    arch=${entry%%:*}
    held=$(flatpak_held "$arch")
    if [[ -n "$held" ]]; then
      case "$(semver_compare "$held" "$version")" in
        0) continue ;;
        1)
          # A Flatpak updates to whatever its ref names, and the member's
          # Dens would then refuse the data a newer one wrote.
          printf "error: the Flatpak repository holds %s for %s, newer than the promoted %s; refusing to move it back\n" \
            "$held" "$arch" "$version" >&2
          exit 1
          ;;
      esac
    fi
    needed+=("$entry")
  done
  write_flatpak_links
  if [[ "${#needed[@]}" -eq 0 ]] && flatpak_published "$version"; then
    publish_flatpak_links
    printf "🟢 The Flatpak repository already holds %s\n" "$version"
    return
  fi

  if [[ "${#needed[@]}" -gt 0 ]]; then
    fetch_flatpak_release "$version"
    for entry in "${needed[@]}"; do
      arch=${entry%%:*}
      run_step "Committed the Flatpak of $version for $arch" "Failed to commit the Flatpak of $version for $arch" \
        env ARCH="$arch" GPG_KEY="$FLATPAK_KEYS" bash "$CI_SCRIPT_DIR/flatpak.sh" commit \
          "$FLATPAK_WORK/release/dens-$arch" "$version" "$FLATPAK_REPO"
    done
  fi
  run_step "Wrote the Flatpak repository's deltas and summary" "Failed to write the Flatpak repository's deltas and summary" \
    env GPG_KEY="$FLATPAK_KEYS" TRUSTED_KEYS="$FLATPAK_TRUSTED" bash "$CI_SCRIPT_DIR/flatpak.sh" update "$FLATPAK_REPO"
  if ! check_flatpak_summary "$FLATPAK_REPO"; then
    printf "error: the new Flatpak summary doesn't check out against the Flatpak's key\n" >&2
    exit 1
  fi
  upload_flatpak_repository
  check_flatpak_upload
  publish_flatpak_links
  record_flatpak_publication "$version"
  printf "🟢 Published the Flatpak of %s\n" "$version"
}
