#!/usr/bin/env bash
# The M15.3 check's den: alice's install hosts it, with #general, where an
# old message stands above forty that bob wrote, so a page reads in the
# middle with it out of view, and alice's newest to react to; a DM between
# alice and bob, where alice says something; and an emoji of the den's,
# :party:, which alice added.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
dm=$(checked_dm alice bob)
./node browser/make-emoji.mjs party.png 96 96
api alice POST "/api/dens/$DEN/emoji" "{\"name\":\"party\",\"upload\":\"$(upload alice party.png party.png)\"}" >/dev/null
old=$(send alice "$general" 'An old message, far above the view')
for i in $(seq 1 40); do send bob "$general" "Filler $i, a line to read past" >/dev/null; done
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "DM=$dm"
echo "OLD=$old"
echo "MSG=$(send alice "$general" 'React to this')"
echo "DM_MSG=$(send alice "$dm" 'A DM to react to')"
