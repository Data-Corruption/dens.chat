#!/usr/bin/env bash
# The M7.2 check's den: alice's install hosts it, with its voice channel,
# Lounge.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
