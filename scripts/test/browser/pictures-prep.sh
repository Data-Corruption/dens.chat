#!/usr/bin/env bash
# The M15.1 check's den: alice's install hosts it, with Lounge and
# #general, where alice and bob each say something, bob in it, and a DM
# between them; Firefox's microphone plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
speak
general=$(channel general)
open_dm alice bob >/dev/null
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "ALICE_MSG=$(send alice "$general" 'Hello from alice')"
echo "BOB_MSG=$(send bob "$general" 'Hello from bob')"
