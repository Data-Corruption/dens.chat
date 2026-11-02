#!/usr/bin/env bash
# The M14.3 check's den: alice's install hosts it, with Lounge, and bob in
# it; Firefox's microphone plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
speak
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
echo "BOB_ID=$(api bob GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')"
