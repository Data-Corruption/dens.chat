#!/usr/bin/env bash
# The steam check's den: a text channel, where alice posts links to games
# on Steam.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
echo "DEN=$DEN"
echo "GENERAL=$general"
