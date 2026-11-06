#!/usr/bin/env bash
# The M17.3 check's den: alice's install hosts it, with #general, and bob
# joins; bob's laptop, m2e, signs in as him through his first device's
# approval, so it holds his seal, which his notes' key comes from. Neither
# of bob's Dens has opened his notes yet.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
laptop
for who in bob laptop; do
    for _ in $(seq 80); do
        [[ $(api "$who" GET "/api/dens/$DEN/state" | json "bool(d.get('has_notes'))") == True ]] && break
        sleep 0.25
    done
done
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "BOB_ID=$(api bob GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
