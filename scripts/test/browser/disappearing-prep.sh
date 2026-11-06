#!/usr/bin/env bash
# The M16.4 check's den: alice's install hosts it, a development instance's,
# whose den takes times of seconds; bob and carol join, each Dens
# publishing its member's identity, and a DM between alice and bob opens
# at once.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
for who in alice bob carol; do
    for _ in $(seq 80); do
        [[ $(api "$who" GET "/api/dens/$DEN/state" |
            json "d.get('has_group_dms') and d.get('disappearing') and all(m.get('identity') for m in d['members'] if not m.get('left_at'))") == True ]] && break
        sleep 0.25
    done
done
dm=$(open_dm alice bob)
echo "DEN=$DEN"
echo "DM=$dm"
for who in alice bob carol; do
    echo "${who^^}_ID=$(api "$who" GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
done
