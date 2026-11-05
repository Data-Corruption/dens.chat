#!/usr/bin/env bash
# The M4.1 check's den: two text channels, and a DM between alice and bob,
# which opens at once (M16.1).
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
other=$(channel other)
dm=$(open_dm alice bob)
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "OTHER=$other"
echo "DM=$dm"
