#!/usr/bin/env bash
# The M16.1 check's den: alice's install hosts it, bob joins, and a DM
# between them opens at once, neither having written yet; bob's laptop,
# m2e, signs in as him through his first device's approval, so it holds
# his seal and the record of what his Dens kept.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
dm=$(open_dm alice bob)
laptop
echo "DEN=$DEN"
echo "DM=$dm"
echo "ALICE_ID=$(api alice GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
echo "BOB_ID=$(api bob GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
