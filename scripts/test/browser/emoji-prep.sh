#!/usr/bin/env bash
# The M15.2 check's den: alice's install hosts it, with #general, where
# alice says something, and a DM between alice and bob; and the images
# alice adds as emoji: a still face larger than an emoji, and an animated
# GIF.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
dm=$(checked_dm alice bob)
./node browser/make-emoji.mjs "Party Face.png" 256 160
./node browser/make-emoji.mjs dance.gif 160 160
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "DM=$dm"
echo "ALICE_MSG=$(send alice "$general" 'Hello from alice')"
