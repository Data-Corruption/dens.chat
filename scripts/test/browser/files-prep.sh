#!/usr/bin/env bash
# The M5.2 check's den: bob posts three photos in one message, and a clip
# with text in another; and from M17.2 alice and bob have a DM, for a
# spoiler sent in it; and from M21.1 alice posts an animated GIF in #gifs,
# which 45 messages of three lines after it push far up the channel, yet
# within the page the channel loads first.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
echo "THREE=$(send bob "$general" "three photos" "$(upload bob photo.jpg one.jpg)" "$(upload bob photo.jpg two.jpg)" "$(upload bob photo.jpg three.jpg)")"
echo "CLIP=$(send bob "$general" "a clip" "$(upload bob clip.mp4 clip.mp4)")"
echo "DM=$(open_dm alice bob)"
./node browser/make-emoji.mjs moving.gif 400 300
gifs=$(channel gifs)
echo "GIF=$(send alice "$gifs" "a gif" "$(upload alice moving.gif moving.gif)")"
for i in $(seq 45); do
    if ((i % 2)); then who=bob; else who=alice; fi
    send "$who" "$gifs" "after the gif, $i\\nits second line\\nand its third" >/dev/null
done
echo "GIFS=$gifs"
echo "DEN=$DEN"
echo "GENERAL=$general"
