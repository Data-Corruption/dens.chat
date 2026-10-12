#!/usr/bin/env bash
# The M6.2 check's den: 30 messages from alice in general, and one to edit
# and one to delete while bob's service is off.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
for i in $(seq 30); do send alice "$general" "message $i" >/dev/null; done
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "EDIT=$(send alice "$general" "to be edited")"
echo "DELETE=$(send alice "$general" "to be deleted")"
