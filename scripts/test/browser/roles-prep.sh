#!/usr/bin/env bash
# The roles check's den: alice's, with bob and carol its members; general,
# raids (text) and Raid Voice (voice).
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
general=$(channel general)
raids=$(channel raids)
raid_voice=$(channel "Raid Voice" voice)
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "RAIDS=$raids"
echo "RAID_VOICE=$raid_voice"
