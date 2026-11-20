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
# A check cut short leaves its drivers and browsers running, the browsers
# on the debugging ports the next one's take. Checks run one at a time, so
# whatever runs now is left over. By the path each starts with, which this
# script's own arguments, chromium or firefox, can't match; Node names its
# process MainThread, so not by name.
left='^(([^ ]*/)?node |/usr/(lib/(chromium|firefox-esr)/|bin/(chromium|firefox-esr)( |$)))'
pkill -u "$UID" -f "$left" || :
for _ in $(seq 50); do
    pgrep -u "$UID" -f "$left" >/dev/null || break
    sleep 0.2
done
pkill -KILL -u "$UID" -f "$left" || :
bash "./$check-prep.sh" >"$out"
while IFS='=' read -r name value; do
    [[ $name =~ ^[A-Z_]+$ && $value =~ ^[A-Za-z0-9_.:/-]*$ ]] || { echo "unexpected prep output: $name=$value" >&2; exit 1; }
    export "$name=$value"
    # RANDOM, and some other names bash keeps for itself, read back as
    # bash's value instead.
    [[ ${!name} == "$value" ]] || { echo "prep output $name is a name bash keeps for itself" >&2; exit 1; }
done <"$out"
exec "$HOME/node" "./drive-$check.mjs" "$@"
