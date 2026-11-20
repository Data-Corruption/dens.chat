#!/usr/bin/env bash
# The two-shares check's den: alice's install hosts it, with Lounge, and
# carol joins, whom the voice probe plays; Firefox's microphone plays
# speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
speak
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
