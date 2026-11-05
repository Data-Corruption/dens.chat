#!/usr/bin/env bash
# The M14.2 check's den: alice's install hosts it, with Lounge and general,
# and a DM between bob and alice, which opens at once (M16.1); Firefox's
# microphone plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
speak
general=$(channel general)
dm=$(open_dm bob alice)
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "DM=$dm"
