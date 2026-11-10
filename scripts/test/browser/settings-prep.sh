#!/usr/bin/env bash
# The M19.1 check's den: alice's install hosts it, with general, a text
# channel bob has written in, so a message list has rows to measure, a
# phone's photo among them and a link to a YouTube video.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
for i in 1 2 3 4 5; do send bob "$general" "Bob's message $i, long enough to show where the list's edge is as it wraps under a narrower width." >/dev/null; done
photo=$(send bob "$general" "A photo from my phone." "$(upload bob photos/pixel-2.jpg photo.jpg)")
send bob "$general" "https://www.youtube.com/watch?v=dQw4w9WgXcQ" >/dev/null
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "PHOTO=$photo"
