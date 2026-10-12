#!/usr/bin/env bash
# The M5.5 check's den, with a #general.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
echo "GENERAL=$(channel general)"
echo "DEN=$DEN"
