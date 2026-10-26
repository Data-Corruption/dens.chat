#!/usr/bin/env bash
# The M11.1 check's den: alice's install hosts it, with its voice channel
# Lounge; general, a text channel bob has written in; the group Games, with
# the text channel clips and the voice channel Arcade; and a DM from alice
# to bob.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
for i in 1 2 3; do send bob "$general" "Bob's message $i" >/dev/null; done
games=$(group Games)
clips=$(channel clips text "$games")
channel Arcade voice "$games" >/dev/null
bob_id=$(api alice GET "/api/dens/$DEN/state" | json "next(m['id'] for m in d['members'] if m['username'] == 'bob')")
dm=$(api alice POST "/api/dens/$DEN/dms" "{\"member_id\":\"$bob_id\"}" | json 'd["id"]')
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "CLIPS=$clips"
echo "DM=$dm"
