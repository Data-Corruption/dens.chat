#!/usr/bin/env bash
# The M20.1 check's den: alice's install hosts it, with Lounge, and bob
# joins; bob's laptop, m2e, signs in as him through his first device's
# approval, so both hold his seal, which the boxes between them are sealed
# with. Keybind relay is on in both of bob's installs. Firefox's microphone
# plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
laptop
speak
for who in bob laptop; do
    api "$who" POST /api/settings '{"relayKeys":true}' >/dev/null
done
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
