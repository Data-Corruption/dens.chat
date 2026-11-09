#!/usr/bin/env bash
# The M19.1 check's den: alice's install hosts it, with general, a text
# channel bob has written in, so a message list has rows to measure.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
for i in 1 2 3 4 5; do send bob "$general" "Bob's message $i, long enough to show where the list's edge is as it wraps under a narrower width." >/dev/null; done
echo "DEN=$DEN"
echo "GENERAL=$general"
