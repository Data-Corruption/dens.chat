#!/usr/bin/env bash
# Runs one check in the container, as tester: its prep script starts the
# development instances afresh and prints what its driver needs as NAME=ID
# lines, which the driver then gets in its environment. run.sh calls this.
#
#   browser/check.sh CHECK [ARGS...]

set -euo pipefail
cd "$HOME/browser"
check=$1
shift
out=$(mktemp)
trap 'rm -f "$out"' EXIT
bash "./$check-prep.sh" >"$out"
while IFS='=' read -r name value; do
    [[ $name =~ ^[A-Z_]+$ && $value =~ ^[A-Za-z0-9_.:/-]*$ ]] || { echo "unexpected prep output: $name=$value" >&2; exit 1; }
    export "$name=$value"
done <"$out"
exec "$HOME/node" "./drive-$check.mjs" "$@"
