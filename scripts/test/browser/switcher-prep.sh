#!/usr/bin/env bash
# The switcher check's dens. Alice's install hosts hers: general, the
# voice channel Stage, random, and long, each but Stage with alice's
# messages bob hasn't read, long's sixty, and staff, a staff-only channel
# bob can't see. Carol's install hosts Second Den, which bob joins too,
# with carol's welcome in its lobby.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
stage=$(channel Stage voice)
random=$(channel random)
long=$(channel long)
api alice POST "/api/dens/$DEN/channels" '{"name":"staff","kind":"text","staff_only":true}' >/dev/null
for _ in $(seq 80); do
    staff=$(api alice GET "/api/dens/$DEN/state" | json "next((c['id'] for c in d['channels'] if c['name'] == 'staff'), '')")
    [[ -n $staff ]] && break
    sleep 0.25
done
send alice "$general" "Hello from general" >/dev/null
send alice "$random" "Hello from random" >/dev/null
for i in $(seq 60); do send alice "$long" "Long line $i" >/dev/null; done

start m2c --port 18487 --den-port 18488 --media-udp-port 17883 --media-tcp-port 17884
pair carol
api carol POST /api/den '{"name":"Second Den","url":"http://127.0.0.1:18488","username":"carol","display_name":"Carol","password":"den password"}' >/dev/null
den2=$(api carol GET /api/dens | json 'next(x["den_id"] for x in d["dens"] if x["own"])')
invite2=$(api carol POST "/api/dens/$den2/invites" '{"expires_in":3600,"max_uses":1}' | json 'd["invite"]')
api bob POST /api/dens/join "{\"invite\":\"$invite2\",\"username\":\"bob\",\"display_name\":\"Bob\",\"password\":\"bob password\"}" >/dev/null
api carol POST "/api/dens/$den2/channels" '{"name":"lobby","kind":"text"}' >/dev/null
for _ in $(seq 80); do
    lobby=$(api bob GET "/api/dens/$den2/state" | json "next((c['id'] for c in d['channels'] if c['name'] == 'lobby'), '')")
    [[ -n $lobby ]] && break
    sleep 0.25
done
api carol POST "/api/dens/$den2/channels/$lobby/messages" "{\"nonce\":\"$(nonce)\",\"text\":\"Welcome to the lobby\"}" >/dev/null

echo "DEN=$DEN"
echo "SECOND_DEN=$den2"
echo "GENERAL=$general"
echo "RANDOM_CHANNEL=$random"
echo "LONG=$long"
echo "STAGE=$stage"
echo "STAFF=$staff"
echo "LOBBY=$lobby"
