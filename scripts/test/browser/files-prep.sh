#!/usr/bin/env bash
# The M5.2 check's den: bob posts three photos in one message, and a clip
# with text in another; and from M17.2 alice and bob have a DM, for a
# spoiler sent in it.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
echo "THREE=$(send bob "$general" "three photos" "$(upload bob photo.jpg one.jpg)" "$(upload bob photo.jpg two.jpg)" "$(upload bob photo.jpg three.jpg)")"
echo "CLIP=$(send bob "$general" "a clip" "$(upload bob clip.mp4 clip.mp4)")"
echo "DM=$(open_dm alice bob)"
echo "DEN=$DEN"
echo "GENERAL=$general"
