#!/usr/bin/env bash
# The fixes check's den: alice's install hosts it, with its voice channel
# Lounge; general, a text channel bob has written in; the group Games, with
# the text channel clips and the voice channel Arcade; and a DM from alice
# to bob. Bob is a moderator, his name too long for its row, and
# his first message has a reaction, and clips a photo and a
# clip of his.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
first=$(send bob "$general" "Bob's message 1")
for i in 2 3; do send bob "$general" "Bob's message $i" >/dev/null; done
api bob POST "/api/dens/$DEN/messages/$first/reactions?channel=$general" '{"emoji":"👍","on":true}' >/dev/null
games=$(group Games)
clips=$(channel clips text "$games")
send bob "$clips" "a photo and a clip" "$(upload bob photo.jpg photo.jpg)" "$(upload bob clip.mp4 clip.mp4)" >/dev/null
channel Arcade voice "$games" >/dev/null
bob_id=$(api alice GET "/api/dens/$DEN/state" | json "next(m['id'] for m in d['members'] if m['username'] == 'bob')")
api alice PUT "/api/dens/$DEN/members/$bob_id/role" '{"role":"moderator"}' >/dev/null
api bob PATCH "/api/dens/$DEN/me" '{"display_name":"Bartholomew Fitzwilliam-Jones"}' >/dev/null
dm=$(api alice POST "/api/dens/$DEN/dms" "{\"member_id\":\"$bob_id\"}" | json 'd["id"]')
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "CLIPS=$clips"
echo "DM=$dm"
