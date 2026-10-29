#!/usr/bin/env bash
# The M12.2 check's den: alice's install hosts it, with Lounge; Firefox's
# microphone plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
speak
echo "DEN=$DEN"
