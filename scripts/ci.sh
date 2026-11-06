#!/usr/bin/env bash

# Release automation used by .github/workflows/release.yml.
#
#   ./scripts/ci.sh --plan
#     Check the release host and Git tag, then report which tests are needed
#     and whether a release job should run. Does not build binaries or change
#     the release host or Git repository. Before the first release (no
#     CHANGELOG.md heading and no version tag) it asks for full validation
#     and no release, and needs no credentials.
#
#   ./scripts/ci.sh --plan-pr
#     Report which test jobs a pull request needs, from the files it changes:
#     its merge commit, which the workflow checks out, against the first
#     parent. Needs no credentials, and never asks for a release.
#
#   ./scripts/ci.sh --execute
#     Build and publish the CHANGELOG version, or finish an interrupted release.
#     The workflow runs this only after the tests requested by --plan pass, in
#     the android environment, whose ANDROID_KEYSTORE and
#     ANDROID_KEYSTORE_PASSWORD sign the Android app's APK (M12.4).
#
#   ./scripts/ci.sh --flatpak
#     Publish the promoted release's Flatpak to its repository on the release
#     host, or finish an interrupted publication (scripts/ci/flatpak.sh). The
#     workflow runs this after --execute, in the flatpak environment, whose
#     FLATPAK_GPG_KEY signs it, where flatpak and ostree are.
#
# Otherwise every command but --plan-pr needs GITHUB_REPOSITORY, the R2
# credentials, and rclone on PATH.
# Execution also needs the build toolchain and GitHub's signing/tag permissions.
# Run from the repository root. Project settings stay in scripts/build.sh.

set -euo pipefail

CI_SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# Sourcing shares settings and build helpers without running a local build.
# shellcheck source=build.sh
source "$CI_SCRIPT_DIR/build.sh"
# shellcheck source=ci/release.sh
source "$CI_SCRIPT_DIR/ci/release.sh"
# shellcheck source=ci/flatpak.sh
source "$CI_SCRIPT_DIR/ci/flatpak.sh"

NO_CACHE='Cache-Control: no-store, max-age=0, must-revalidate'
PUBLISH_REMOTE=""
RCLONE_ARGS=()
UPLOAD_ARGS=()

# resolve_release_policy fills these from the release host and Git remote.
RELEASE_BUILD_REQUIRED=true
RELEASE_CURRENT_VERSION=""
RELEASE_INSTALLERS_TO_PUBLISH=()
RELEASE_TARGET_TAG_EXISTS=false
RELEASE_TAG_ONLY=false

configure_release_identity() {
  if [[ -z "${GITHUB_REPOSITORY:-}" ]]; then
    printf "error: GITHUB_REPOSITORY is required to identify the release signer\n" >&2
    exit 1
  fi
  CERT_IDENTITY="https://github.com/${GITHUB_REPOSITORY}/.github/workflows/release.yml@refs/heads/main"
}

prepare_release_context() {
  MODE="ci"
  BUILD_KIND="prod-all"
  DEV_MODE=false
  # The release job builds only a version this commit's test jobs passed,
  # linux-tests' Go suite among them, which it waits for.
  RUN_TESTS=false
  configure_release_identity
  validate_app_name
  detect_host_arch
  validate_pins
  require_distribution_config
  resolve_version
  validate_version "$VERSION"
  VERSION_DIR="$RELEASE_DIR/releases/$VERSION"
  configure_distribution
  clean_out_dir
  vendor_cosign
}

# write_plan E2E MEDIA INSTALLER_TESTS RELEASE reports the plan in the log
# and to the workflow. E2E asks for the test jobs, and MEDIA for the two of
# them that check the media module alone, media-module and media-fuzz.
write_plan() {
  local plan
  plan=$(printf 'e2e_required=%s\nmedia_required=%s\ninstaller_tests_required=%s\nrelease_required=%s' "$1" "$2" "$3" "$4")
  printf '%s\n' "$plan"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    printf '%s\n' "$plan" >> "$GITHUB_OUTPUT"
  fi
}

# The workflow uses these decisions to schedule tests. An upload without a
# Git tag needs E2E: the earlier attempt may not have passed its tests. A
# push asks for the media jobs with the rest, since a release waits for
# every job.
plan_release() {
  local e2e_required=false installer_tests_required=false
  if ! $RELEASE_TAG_ONLY; then
    if ! $RELEASE_TARGET_TAG_EXISTS; then
      e2e_required=true
    fi
    package_installers
    plan_installer_publication
    if [[ "${#RELEASE_INSTALLERS_TO_PUBLISH[@]}" -gt 0 ]]; then
      installer_tests_required=true
    fi
  fi

  write_plan "$e2e_required" "$e2e_required" "$installer_tests_required" true
}

# pr_changed_files prints the files a pull request changes, each ended by a
# NUL: its merge commit, which the workflow checks out, against the first
# parent, the base it merges into. A rename lists both its paths, so code
# moved under docs/ still counts as a change to the code. It fails unless
# HEAD is a merge, as a PR's checkout is: against a branch's own commit, the
# first parent would show only that commit's part of the PR.
pr_changed_files() {
  git rev-parse --quiet --verify 'HEAD^2^{commit}' >/dev/null || return 1
  git diff --no-renames --name-only -z 'HEAD^1' HEAD
}

# plan_pr_jobs [PATH...] reports the test jobs a pull request that changes
# the PATHs needs. Docs alone need none: no test reads them but the site's
# icon, which the Docs workflow's build checks (scripts/test.sh -docs). The
# media module's inputs, and CI's own files, which decide what runs, need
# every job. Anything else needs every job but the two that check the media
# module alone. CHANGELOG.md isn't docs: releases are cut from it. No paths
# at all prove nothing, so they get every job too.
plan_pr_jobs() {
  local e2e=false media=false path why=""
  if [[ $# -eq 0 ]]; then
    e2e=true media=true why="no changed files listed"
  fi
  # In order: a case pattern's * matches a slash too, so the root's
  # markdown comes after everything in a directory.
  for path in "$@"; do
    case "$path" in
      .github/* | scripts/ci.sh | scripts/ci/* | \
        internal/media/* | scripts/ffmpeg.sh | scripts/rnnoise.sh | internal/ui/rnnoise/* | \
        internal/ui/assets/wasm/rnnoise.wasm | scripts/vendor.sh | scripts/build/common.sh)
        $media || why="$path is one of the media module's inputs or CI's own files"
        e2e=true media=true
        ;;
      docs/*) ;;
      CHANGELOG.md | */*)
        $e2e || why="$path isn't docs"
        e2e=true
        ;;
      *.md) ;;
      *)
        $e2e || why="$path isn't docs"
        e2e=true
        ;;
    esac
  done
  if $media; then
    printf 'Every job: %s.\n' "$why"
  elif $e2e; then
    printf "Every job but the media module's: %s.\n" "$why"
  else
    printf 'No test job: docs alone.\n'
  fi
  write_plan "$e2e" "$media" false false
}

# plan_pull_request plans a pull request's jobs from the files it changes,
# or every job when they can't be listed.
plan_pull_request() {
  local list files=()
  list=$(mktemp)
  if pr_changed_files > "$list"; then
    mapfile -d '' -t files < "$list"
    rm -f "$list"
    printf 'The PR changes %d files.\n' "${#files[@]}"
    plan_pr_jobs "${files[@]}"
  else
    rm -f "$list"
    printf "Can't list the files this PR changes: every job.\n"
    write_plan true true false false
  fi
}

# Publication order is shared by first attempts and retries. The remote probe
# has already downloaded any verified release we can reuse.
execute_release() {
  if $RELEASE_TAG_ONLY; then
    # A newer version is already public. Finish this older release's tag and
    # cleanup without replacing the current installers or version pointer.
    ensure_promotion_marker
    tag_release
    cleanup_old_releases
    return
  fi

  if $RELEASE_BUILD_REQUIRED; then
    build_binaries
  fi
  package_installers
  if $RELEASE_BUILD_REQUIRED; then
    package_binaries
    package_ffmpeg_source
    package_android
    write_release_version
    generate_checksums
    sign_application_release
    check_signature_as_the_app
    upload_application_release
  fi
  write_root_version_candidate

  plan_installer_publication
  test_changed_installers
  publish_installers

  promote_release
  publish_root_apk
  ensure_promotion_marker
  tag_release
  cleanup_old_releases
}

ci_main() {
  if [[ $# -ne 1 || ( "$1" != "--plan" && "$1" != "--plan-pr" && "$1" != "--execute" && "$1" != "--flatpak" ) ]]; then
    printf 'Usage: ./scripts/ci.sh --plan | --plan-pr | --execute | --flatpak\n' >&2
    return 2
  fi
  case "$1" in
    --flatpak)
      publish_flatpak
      return
      ;;
    --plan-pr)
      plan_pull_request
      return
      ;;
  esac

  # Before the first release there is nothing to publish, so a push to main
  # gets every test job and no release job. Once a version tag exists, a
  # missing heading is a mistake again and planning fails below.
  if [[ "$1" == --plan && -z "$(changelog_version)" ]] && ! released_before; then
    echo "No release heading in CHANGELOG.md and no release yet: validating without publishing."
    write_plan true true false false
    return
  fi

  prepare_release_context
  resolve_release_policy

  case "$1" in
    --plan) plan_release ;;
    --execute) execute_release ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  ci_main "$@"
fi
