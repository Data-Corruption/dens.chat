#!/usr/bin/env bash
# The M6.1 check's den: alice, whose install hosts it, posts a photo and a
# clip in general, which bob's install keeps as his page shows them.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
photo=$(upload alice photo.jpg photo.jpg)
echo "PHOTO_MESSAGE=$(send alice "$general" "a photo" "$photo")"
clip=$(upload alice clip.mp4 clip.mp4)
send alice "$general" "a clip" "$clip" >/dev/null
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "PHOTO=$photo"
echo "CLIP=$clip"
