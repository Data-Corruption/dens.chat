#!/usr/bin/env bash
# The M20 check's den: alice's install hosts it, with Lounge, and bob
# joins; bob's laptop, m2e, signs in as him through his first device's
# approval, so both hold his seal, which the boxes between them are sealed
# with. Keybind relay is on in both of bob's installs, and the laptop
# shares its computer's sound (M20.2). Firefox's microphone plays speech.
# The container's sound is a tone, played into a null sink of its own,
# "tone", which is the default output the laptop's helper reads; the
# browsers play into "speakers", so nothing they play is read again.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
laptop
speak
for who in bob laptop; do
    api "$who" POST /api/settings '{"relayKeys":true}' >/dev/null
done
api laptop POST /api/settings '{"relaySound":true}' >/dev/null
pactl list short sinks | cut -f2 | grep -qx tone || pactl load-module module-null-sink sink_name=tone >/dev/null
pactl set-default-sink tone
if ! { [[ -f tone.pid ]] && kill -0 "$(cat tone.pid)" 2>/dev/null; }; then
    # A sine at 440 Hz, at half of full scale, for ever, paced by pacat.
    nohup bash -c "python3 -c '
import math, struct, sys
period = [int(16384 * math.sin(2 * math.pi * 440 * i / 48000)) for i in range(48000 // 40)]
block = b\"\".join(struct.pack(\"<hh\", v, v) for v in period)
while True:
    sys.stdout.buffer.write(block)
' | pacat -d tone --format=s16le --rate=48000 --channels=2" >/dev/null 2>&1 &
    echo $! >tone.pid
fi
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
