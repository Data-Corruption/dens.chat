#!/usr/bin/env bash
# The M2 and M3 checks' den: alice's install hosts it, with Lounge and
# Games; Firefox's
# microphone plays speech. Carol joins, whom the voice probe plays as a
# third member joining and leaving the sealed call (M16.2).
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
# Games, a second voice channel, which staff's Move to lists (M22.2).
games=$(channel Games voice)
join carol
speak
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
echo "GAMES=$games"
