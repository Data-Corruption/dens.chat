#!/usr/bin/env bash
# The M4.1 check's den: two text channels, and a DM between alice and bob
# that both have checked.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
other=$(channel other)
bob_id=$(api bob GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')
dm=$(api alice POST "/api/dens/$DEN/dms" "{\"member_id\":\"$bob_id\"}" | json 'd["id"]')
check() { api "$1" GET "/api/dens/$DEN/dms/$dm/check"; }
if [[ $(check alice | json 'd["stage"]') == none ]]; then
    api alice POST "/api/dens/$DEN/dms/$dm/key" '{}' >/dev/null
fi
a="" b=""
for _ in $(seq 60); do
    a=$(check alice | json 'd.get("half", "")')
    b=$(check bob | json 'd.get("half", "")')
    [[ -n $a && -n $b ]] && break
    sleep 0.5
done
[[ -n $a && -n $b ]] || { echo "the DM's check code never came" >&2; exit 1; }
api alice POST "/api/dens/$DEN/dms/$dm/check" "{\"digits\":\"$b\"}" >/dev/null
api bob POST "/api/dens/$DEN/dms/$dm/check" "{\"digits\":\"$a\"}" >/dev/null
for _ in $(seq 20); do
    [[ $(check bob | json 'd["checked"] and d["partner"]') == True ]] && break
    sleep 0.5
done
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "OTHER=$other"
echo "DM=$dm"
