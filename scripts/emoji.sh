#!/usr/bin/env bash

# Writes the list of Unicode's emoji (M15.2) from its pinned inputs in
# scripts/vendor.sh, Emoji's test file and CLDR's English annotations: the
# page's, which its picker and completion read (internal/ui/assets/
# emoji.json), and denproto's, which reactions are checked against
# (internal/denproto/emojilist.go). The repository keeps both, so CI writes
# them again and fails on a difference. scripts/emoji/ is the generator.
#
# The list stops at UPTO, the newest Emoji version that Windows 11 and Noto
# Color Emoji both draw everywhere, since the page draws emoji in the
# system's font: Windows 11 has drawn Emoji 16.0 since 24H2, and 17.0 only
# reaches it as an update rolls out. Raise it once both draw the next.
#
# Usage:
#   ./scripts/emoji.sh           # write both lists
#   ./scripts/emoji.sh --check   # write them elsewhere, and fail if they differ

set -euo pipefail
exec </dev/null
umask 022
export LC_ALL=C

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

UPTO=16.0
PAGE=internal/ui/assets/emoji.json
GO=internal/denproto/emojilist.go

# generate PAGE GO: write the page's list into PAGE and denproto's into GO.
generate() {
  local data
  data=$(./scripts/vendor.sh emoji-data | sed -n 's/^emoji-data=//p')
  [[ -n "$data" ]] || { echo "error: vendor.sh returned no emoji data" >&2; return 1; }
  go run ./scripts/emoji -upto "$UPTO" -test "$data/emoji-test.txt" -annotations "$data/annotations-en.xml" \
    -derived "$data/annotations-derived-en.xml" -page "$1" -go "$2"
  gofmt -l "$2" | grep -q . && { echo "error: $2 isn't formatted as gofmt formats it" >&2; return 1; }
  return 0
}

mkdir -p out
case "${1:-}" in
  "")
    generate "$PAGE" "$GO"
    printf 'wrote %s and %s, up to Emoji %s\n' "$PAGE" "$GO" "$UPTO"
    ;;
  --check)
    work=$(mktemp -d "$ROOT/out/emoji-check.XXXXXX")
    trap 'rm -rf "$work"' EXIT
    generate "$work/emoji.json" "$work/emojilist.go"
    status=0
    for pair in "$work/emoji.json:$PAGE" "$work/emojilist.go:$GO"; do
      if ! cmp -s "${pair%%:*}" "${pair#*:}"; then
        printf "error: %s isn't what its inputs make; run ./scripts/emoji.sh\n" "${pair#*:}" >&2
        status=1
      fi
    done
    [[ $status -eq 0 ]] && printf 'the emoji lists are current\n'
    exit $status
    ;;
  *)
    echo "usage: $0 [--check]" >&2
    exit 2
    ;;
esac
