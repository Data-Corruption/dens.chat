#!/usr/bin/env bash
# The M21.1 check's den: alice's install hosts it, with general, and bob
# joins from his first device; his laptop, m2e, starts paired and in no
# den, for him to sign in to it from its page.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
start m2e --port "${PORT[laptop]}"
pair laptop
echo "DEN=$DEN"
echo "DEN_URL=$DEN_URL"
echo "GENERAL=$general"
