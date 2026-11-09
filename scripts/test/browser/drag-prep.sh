#!/usr/bin/env bash
# The M17.2 drag check's den: alice owns it, with general, Lounge and Games
# in no group, news in the group Talk and Raid in the group Play; bob and
# carol are members.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
general=$(channel general)
games=$(channel Games voice)
talk=$(group Talk)
play=$(group Play)
news=$(channel news text "$talk")
raid=$(channel Raid voice "$play")
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
echo "GENERAL=$general"
echo "GAMES=$games"
echo "TALK=$talk"
echo "PLAY=$play"
echo "NEWS=$news"
echo "RAID=$raid"
