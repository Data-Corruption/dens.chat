#!/usr/bin/env bash
# The M14.2 check's den: alice's install hosts it, with Lounge and general,
# and a DM between bob and alice that both have checked; Firefox's
# microphone plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
speak
general=$(channel general)
dm=$(checked_dm bob alice)
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "DM=$dm"
