#!/usr/bin/env bash
# The M17.4 check's den: alice's install hosts it, with #general, the
# voice channel Lounge, and Backstage, a voice channel staff's alone; bob
# joins. Events are staff's, as a new den has them. For M17.5, a DM
# between alice and bob, where an event's link goes, and the picture
# alice's badge takes.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
dm=$(open_dm alice bob)
./node browser/make-emoji.mjs badge.png 200 200
api alice POST "/api/dens/$DEN/channels" '{"name":"Backstage","kind":"voice","staff_only":true}' >/dev/null
backstage=""
for _ in $(seq 80); do
    backstage=$(api alice GET "/api/dens/$DEN/state" | json "next((c['id'] for c in d['channels'] if c['name'] == 'Backstage'), '')")
    [[ -n $backstage ]] && break
    sleep 0.25
done
[[ -n $backstage ]] || { echo "alice never saw Backstage" >&2; exit 1; }
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "LOUNGE=$LOUNGE"
echo "BACKSTAGE=$backstage"
echo "DM=$dm"
