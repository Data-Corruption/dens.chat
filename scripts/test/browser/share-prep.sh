#!/usr/bin/env bash
# The M4.2 check's den: alice's install hosts it, with Lounge; Firefox's
# microphone plays speech. Carol joins, whose Chromium watches alice's
# share beside bob's Firefox (M18).
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
speak
echo "DEN=$DEN"
