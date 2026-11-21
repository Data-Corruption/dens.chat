#!/usr/bin/env bash
# The calls check's den: alice's install hosts it, with Lounge and
# Games; Firefox's
# microphone plays speech. Carol joins, whom the voice probe plays as a
# third member joining and leaving the sealed call.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
# Games, a second voice channel, which staff's Move to lists.
games=$(channel Games voice)
join carol
speak
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
echo "GAMES=$games"
