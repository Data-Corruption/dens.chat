#!/usr/bin/env bash
# The M15.4 check's den: alice's install hosts it, with #general, and bob
# in it. The driver posts the polls, alice's first through the composer.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "ALICE_ID=$(api alice GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
echo "BOB_ID=$(api bob GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
