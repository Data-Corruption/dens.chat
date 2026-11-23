#!/usr/bin/env bash
# The audit check's den: alice's, with bob, carol and dave its members;
# general, where bob and dave have each written, and doomed, a channel to
# delete.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
join dave
general=$(channel general)
doomed=$(channel doomed)
words=$(send bob "$general" "bobs words, never in the log")
send dave "$general" "dave was here" >/dev/null
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "DOOMED=$doomed"
echo "WORDS=$words"
