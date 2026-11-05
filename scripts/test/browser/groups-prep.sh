#!/usr/bin/env bash
# The M16.3 check's den: alice's install hosts it, and bob, carol and dave
# join, each Dens publishing its member's identity as it signs in, so any
# of them can make a group with the others, or be added to one.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
join dave
for who in alice bob carol dave; do
    for _ in $(seq 80); do
        [[ $(api "$who" GET "/api/dens/$DEN/state" |
            json "d.get('has_group_dms') and all(m.get('identity') for m in d['members'] if not m.get('left_at'))") == True ]] && break
        sleep 0.25
    done
done
echo "DEN=$DEN"
for who in alice bob carol dave; do
    echo "${who^^}_ID=$(api "$who" GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
done
