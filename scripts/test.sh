#!/usr/bin/env bash

# Public test entrypoint.
#
# Usage:
#   ./scripts/test.sh                              # race-enabled Go tests
#   ./scripts/test.sh -lint                        # gofmt, go vet for each OS, and shellcheck
#   ./scripts/test.sh -js                          # the page's tests on the pinned Node
#   ./scripts/test.sh -docs                        # the site built as the Docs workflow builds it, and its icon checked
#   ./scripts/test.sh -release                     # release state machine
#   ./scripts/test.sh -e2e [lifecycle options]     # Linux lifecycle E2E
#   ./scripts/test.sh -den-e2e [options]           # Linux den E2E: join through Caddy
#   ./scripts/test.sh -flatpak-e2e                 # the Flatpak's publisher, then the Flatpak, made from this tree, in containers
#   ./scripts/test.sh -android                     # the Android app built: vetted for Android, its APKs, its host tests
#   ./scripts/test.sh -android-e2e [--no-build]    # the Android app, made from this tree, in Android's emulator
#   ./scripts/test.sh -windows                     # Go tests on the Windows host (WSL only)
#   ./scripts/test.sh -all                         # every available suite
#
# Lifecycle options are forwarded to test-lifecycle-e2e.sh, for example:
#   ./scripts/test.sh -e2e --distros "debian arch"

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
# shellcheck source=build/go.sh
source scripts/build/go.sh

usage() {
  cat <<'EOF'
Usage: ./scripts/test.sh [mode]

With no argument, run the race-enabled Go test suite.
  -lint     Check every Go file is gofmt's, vet for Linux, Windows and Android, and run the pinned shellcheck over every script
  -js       Run the page's tests on the pinned Node
  -docs     Build the site as the Docs workflow does, failing on any warning, and check its icon is the app's mark
EOF
  cat <<'EOF'
  -release  Test the release publication state machine
  -e2e      Test the Linux lifecycle; remaining arguments go to test-lifecycle-e2e.sh
  -den-e2e  Test joining a den through Caddy; remaining arguments go to test-den-e2e.sh
  -flatpak-e2e  Test the Flatpak's publisher, then the Flatpak, made from this tree's binary and published by it, in containers
  -android  Build the Android app: Go vetted for Android, the development APKs, and the host package's tests for the emulator
  -android-e2e  Test the Android app, made from this tree, in Android's emulator, joining a den in a container; --no-build uses out/'s builds
  -windows  From WSL, run the Go tests natively on the Windows host
  -all      Run every available suite
EOF
}

require_no_args() {
  local mode=$1
  shift
  if [[ $# -ne 0 ]]; then
    printf "error: %s does not accept additional arguments\n" "$mode" >&2
    usage >&2
    exit 2
  fi
}


run_go_tests() {
  command -v go >/dev/null 2>&1 || {
    printf "error: 'go' is required but not installed or not in \$PATH\n" >&2
    exit 1
  }
  command -v gcc >/dev/null 2>&1 || {
    printf "error: 'gcc' is required by go test -race\n" >&2
    exit 1
  }

  go test -race ./...
  # The race detector slows the media module's workers fifty times over and
  # finds nothing in them, so its long cases run without it.
  go test ./internal/media/...
}

# run_windows_tests cross-compiles every package's tests and runs them on
# the Windows host through WSL interop, as the desktop user, each in a copy
# of its package's directory holding the repository's testdata, which tests
# read by relative paths. The cmd package checks the source tree and
# scripts, which only exist on the Linux side, so it stays with the Linux
# suite.
run_windows_tests() {
  command -v powershell.exe >/dev/null 2>&1 || {
    printf "error: -windows needs WSL with Windows interop\n" >&2
    exit 1
  }
  local win_temp work pkg name rel dir failed=0
  win_temp=$(powershell.exe -NoProfile -NonInteractive -Command '[IO.Path]::GetTempPath()' | tr -d '\r')
  work=$(wslpath -u "${win_temp}dens-go-tests")
  rm -rf "$work"
  mkdir -p "$work"
  while IFS= read -r dir; do
    mkdir -p "$work/$(dirname "$dir")"
    cp -r "$dir" "$work/$dir"
  done < <(find internal pkg -type d -name testdata -not -path '*/testdata/*')
  for pkg in $(go list ./... | grep -v '/cmd$'); do
    rel=${pkg#"$(go list -m)"/}
    name=$(printf '%s' "$rel" | tr '/' '_')
    mkdir -p "$work/$rel"
    GOOS=windows go test -c -o "$work/$rel/$name.test.exe" "$pkg"
  done
  while IFS= read -r test_exe; do
    # The command registry's test, the notices', the Windows resource
    # objects', the protocol's error codes', the mark's copies' and the
    # emoji lists' read the module's source, which the Windows side doesn't
    # have.
    if (cd "$(dirname "$test_exe")" && timeout 600 "$test_exe" -test.count=1 \
      -test.skip 'TestAllCommandConstructorsAreListed|TestNoticesNameEveryModule|TestCommittedObjects|TestKnownCodes|TestMarkCopiesAreTheMark|TestEmojiListsAgree' </dev/null >"$test_exe.log" 2>&1); then
      printf 'ok    %s\n' "$(basename "$test_exe" .test.exe)"
    else
      printf 'FAIL  %s\n' "$(basename "$test_exe" .test.exe)"
      tail -n 20 "$test_exe.log"
      failed=1
    fi
  done < <(find "$work" -name '*.test.exe' | sort)
  rm -rf "$work"
  return "$failed"
}

# run_lint checks every Go file is formatted as go.mod's gofmt formats it,
# vets the module for each OS Dens builds for, whose files only that OS
# compiles, and runs shellcheck over every tracked script. The build's and
# CI's libraries are left out: --external-sources follows them through
# `source` from the scripts that define their variables.
run_lint() {
  local files unformatted
  mapfile -d '' files < <(git ls-files -z '*.go')
  unformatted=$(go_fmt -l "${files[@]}")
  if [[ -n "$unformatted" ]]; then
    printf "error: gofmt would change:\n%s\n" "$unformatted" >&2
    exit 1
  fi
  printf '🟢 gofmt passed (%d files)\n' "${#files[@]}"
  go vet ./...
  GOOS=windows go vet ./...
  GOOS=android GOARCH=arm64 go vet ./...
  printf '🟢 go vet passed for Linux, Windows and Android\n'

  local shellcheck_bin scripts
  shellcheck_bin=$(./scripts/vendor.sh shellcheck | sed -n 's/^shellcheck=//p')
  if [[ -z "$shellcheck_bin" || ! -x "$shellcheck_bin" ]]; then
    printf "error: vendor.sh returned no executable shellcheck path\n" >&2
    exit 1
  fi
  mapfile -d '' scripts < <(git ls-files -z '*.sh' ':!scripts/build/*.sh' ':!scripts/ci/*.sh' ':!scripts/test/incus.sh')
  "$shellcheck_bin" --external-sources --source-path=scripts --source-path=scripts/build "${scripts[@]}"
  printf '🟢 shellcheck passed (%d scripts)\n' "${#scripts[@]}"
}

# The page's tests under internal/ui/test. esbuild bundles each with the
# page's own Preact, as the build does, and Node's built-in runner runs the
# bundles, so no npm packages are involved.
run_js_tests() {
  local paths esbuild preact node
  paths=$(./scripts/vendor.sh esbuild preact node)
  esbuild=$(sed -n 's/^esbuild=//p' <<<"$paths")
  preact=$(sed -n 's/^preact=//p' <<<"$paths")
  node=$(sed -n 's/^node=//p' <<<"$paths")
  rm -rf out/js-tests
  mkdir -p out/js-tests
  local test bundle bundles=()
  for test in internal/ui/test/*.test.js; do
    bundle="out/js-tests/$(basename "${test%.js}").mjs"
    NODE_PATH="$preact" "$esbuild" "$test" --bundle --platform=node --format=esm \
      --jsx=automatic --jsx-import-source=preact --target=es2022 --log-level=warning --outfile="$bundle"
    bundles+=("$bundle")
  done
  "$node" --test --test-reporter=spec "${bundles[@]}"
}

run_release_tests() {
  bash scripts/test-release.sh
}

# run_docs_tests builds the site into docs/out as the Docs workflow does
# before it deploys, with the pinned Hugo, failing on any warning or on code
# its highlighter marks as an error, as an unquoted URL in PowerShell is; then
# runs the one Go test that reads the site: its icon is the app's mark. The
# build clears docs/out first, so a page since removed is neither checked
# nor deployed.
run_docs_tests() {
  local hugo_bin
  hugo_bin=$(./scripts/vendor.sh hugo | sed -n 's/^hugo=//p')
  if [[ -z "$hugo_bin" || ! -x "$hugo_bin" ]]; then
    printf "error: vendor.sh returned no executable Hugo path\n" >&2
    exit 1
  fi
  (
    cd docs
    go mod download
    go mod verify
    HUGO_ENV=production HUGO_ENVIRONMENT=production "$hugo_bin" --gc --minify --panicOnWarning --cleanDestinationDir
  )
  local marked
  marked=$(grep -rlE 'class="?err[" >]' docs/out --include='*.html' || true)
  if [[ -n "$marked" ]]; then
    printf "error: the highlighter marked code as an error in:\n%s\n" "$marked" >&2
    exit 1
  fi
  go test -count=1 -run '^TestMarkCopiesAreTheMark$' ./internal/build
  printf '🟢 Built the site, and its icon is the mark\n'
}

# run_android_build builds the Android app as CI's android-build job does,
# with no emulator: Go vetted for Android, whose _android.go seams nothing
# else compiles; the development APKs, which compile the shell and check its
# calls against the oldest Android it runs on; and the host package's tests
# built for the emulator, as the Android e2e runs them.
run_android_build() {
  GOOS=android GOARCH=arm64 go vet ./...
  ./scripts/build.sh --android
  local tools
  tools=$(./scripts/vendor.sh android | sed -n 's/^android=//p')
  GOOS=android GOARCH=amd64 CGO_ENABLED=1 CC="$tools/ndk/toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android31-clang" \
    go test -c -ldflags=-checklinkname=0 -o /dev/null ./internal/platform/host
  printf '🟢 Built the Android app, and its host tests for the emulator\n'
}

run_lifecycle_e2e() {
  bash scripts/test-lifecycle-e2e.sh "$@"
}

mode=${1:-}
[[ $# -eq 0 ]] || shift

case "$mode" in
  "")
    require_no_args "default" "$@"
    run_go_tests
    ;;
  -lint)
    require_no_args "-lint" "$@"
    run_lint
    ;;
  -js)
    require_no_args "-js" "$@"
    run_js_tests
    ;;
  -docs)
    require_no_args "-docs" "$@"
    run_docs_tests
    ;;
  -release)
    require_no_args "-release" "$@"
    run_release_tests
    ;;
  -e2e)
    run_lifecycle_e2e "$@"
    ;;
  -den-e2e)
    bash scripts/test-den-e2e.sh "$@"
    ;;
  -flatpak-e2e)
    require_no_args "-flatpak-e2e" "$@"
    bash scripts/test-flatpak-e2e.sh
    ;;
  -android)
    require_no_args "-android" "$@"
    run_android_build
    ;;
  -android-e2e)
    bash scripts/test-android-e2e.sh "$@"
    ;;
  -windows)
    require_no_args "-windows" "$@"
    run_windows_tests
    ;;
  -all)
    require_no_args "-all" "$@"
    run_go_tests
    run_lint
    run_js_tests
    run_docs_tests
    run_release_tests
    run_android_build
    run_lifecycle_e2e
    bash scripts/test-den-e2e.sh
    bash scripts/test-flatpak-e2e.sh
    bash scripts/test-android-e2e.sh
    ;;
  -h|--help)
    require_no_args "$mode" "$@"
    usage
    ;;
  *)
    printf "error: unknown test mode '%s'\n" "$mode" >&2
    usage >&2
    exit 2
    ;;
esac
