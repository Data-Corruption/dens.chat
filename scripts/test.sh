#!/usr/bin/env bash

# Public test entrypoint.
#
# Usage:
#   ./scripts/test.sh                              # race-enabled Go tests
#   ./scripts/test.sh -lint                        # shellcheck over the shell scripts
#   ./scripts/test.sh -js                          # the page's tests on the pinned Node
#   ./scripts/test.sh -release                     # release state machine
#   ./scripts/test.sh -e2e [lifecycle options]     # Linux lifecycle E2E
#   ./scripts/test.sh -den-e2e [options]           # Linux den E2E: join through Caddy
#   ./scripts/test.sh -windows                     # Go tests on the Windows host (WSL only)
#   ./scripts/test.sh -all                         # every available suite
#
# Lifecycle options are forwarded to test-lifecycle-e2e.sh, for example:
#   ./scripts/test.sh -e2e --distros "debian arch"

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

usage() {
  cat <<'EOF'
Usage: ./scripts/test.sh [mode]

With no argument, run the race-enabled Go test suite.
  -lint     Run the pinned shellcheck over every shell script
  -js       Run the page's tests on the pinned Node
EOF
  cat <<'EOF'
  -release  Test the release publication state machine
  -e2e      Test the Linux lifecycle; remaining arguments go to test-lifecycle-e2e.sh
  -den-e2e  Test joining a den through Caddy; remaining arguments go to test-den-e2e.sh
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

  ensure_ui_placeholders
  go test -race ./...
  # The race detector slows the media module's workers fifty times over and
  # finds nothing in them, so its long cases run without it.
  go test ./internal/media/...
}

ensure_ui_placeholders() {
  # Generated frontend outputs are gitignored. Empty compile-only placeholders
  # keep ordinary Go tests independent of the frontend toolchain.
  [[ -f internal/ui/assets/css/output.css ]] || : > internal/ui/assets/css/output.css
  [[ -f internal/ui/assets/js/output.js ]] || : > internal/ui/assets/js/output.js
  [[ -f internal/ui/assets/manifest.json ]] ||
    printf '{"css/output.css":"test","js/output.js":"test"}' > internal/ui/assets/manifest.json
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
  ensure_ui_placeholders
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
    # The command registry test reads its package source, which the Windows
    # side can't see.
    if (cd "$(dirname "$test_exe")" && timeout 600 "$test_exe" -test.count=1 -test.skip TestAllCommandConstructorsAreListed </dev/null >"$test_exe.log" 2>&1); then
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

# Entry points only; -x follows the build and CI libraries through `source` so the
# libraries are checked in the context that defines their variables.
run_shell_lint() {
  local shellcheck_bin
  shellcheck_bin=$(./scripts/vendor.sh shellcheck | sed -n 's/^shellcheck=//p')
  if [[ -z "$shellcheck_bin" || ! -x "$shellcheck_bin" ]]; then
    printf "error: vendor.sh returned no executable shellcheck path\n" >&2
    exit 1
  fi
  local scripts=(
    scripts/build.sh
    scripts/ci.sh
    scripts/vendor.sh
    scripts/ffmpeg.sh
    scripts/test.sh
    scripts/test-release.sh
    scripts/test-lifecycle-e2e.sh
    scripts/test/lifecycle-guest.sh
    scripts/test/fixture-releases.sh
    scripts/test-den-e2e.sh
    scripts/test/den-guest.sh
    scripts/dev/seed-chat.sh
    scripts/install.sh
  )
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
    run_shell_lint
    ;;
  -js)
    require_no_args "-js" "$@"
    run_js_tests
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
  -windows)
    require_no_args "-windows" "$@"
    run_windows_tests
    ;;
  -all)
    require_no_args "-all" "$@"
    run_go_tests
    run_shell_lint
    run_js_tests
    run_release_tests
    run_lifecycle_e2e
    bash scripts/test-den-e2e.sh
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
