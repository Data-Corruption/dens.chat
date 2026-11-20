#!/usr/bin/env bash
# The photos check's den, with a #general.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
echo "GENERAL=$(channel general)"
echo "DEN=$DEN"
