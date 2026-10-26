#!/usr/bin/env bash
# The M11.2 check's dens. Alice's install hosts hers: general, where bob
# and alice have written, a message of bob's deleted since, and bob's
# links to messages, one at an address the den left and one to a den
# bob hasn't joined at the den's address; random, with a message of
# alice's; and staff, a staff-only channel bob can't see, with another.
# Carol's install hosts Second Den, which bob joins too, with a link in
# its lobby to a message in alice's.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
random=$(channel random)
api alice POST "/api/dens/$DEN/channels" '{"name":"staff","kind":"text","staff_only":true}' >/dev/null
for _ in $(seq 80); do
    staff=$(api alice GET "/api/dens/$DEN/state" | json "next((c['id'] for c in d['channels'] if c['name'] == 'staff'), '')")
    [[ -n $staff ]] && break
    sleep 0.25
done
bob1=$(send bob "$general" "Bob's first")
send alice "$general" "Alice's first" >/dev/null
bob2=$(send bob "$general" "Bob's second")
gone=$(send bob "$general" "Bob's mistake")
api bob DELETE "/api/dens/$DEN/messages/$gone" >/dev/null
rnd=$(send alice "$random" "In random")
stf=$(send alice "$staff" "Staff only")
send bob "$general" "Bob's third" >/dev/null
# A den's ID, of no den bob joined.
other=NotAJoinedDenNotAJoinedDenNotAJoinedDen0000
send bob "$general" "here $DEN_URL/#m/$DEN/$general/$bob1 there $DEN_URL/#m/$DEN/$random/$rnd staff $DEN_URL/#m/$DEN/$staff/$stf gone $DEN_URL/#m/$DEN/$general/$gone away http://127.0.0.1:18499/#m/$other/1/2 moved http://old.example/#m/$DEN/$general/$bob1 taken $DEN_URL/#m/$other/$general/$bob1" >/dev/null

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
across="over there $DEN_URL/#m/$DEN/$general/$bob2"
DEN=$den2 send bob "$lobby" "$across" >/dev/null

echo "DEN=$DEN"
echo "SECOND_DEN=$den2"
echo "GENERAL=$general"
echo "RANDOM_CHANNEL=$random"
echo "LOBBY=$lobby"
echo "BOB_FIRST=$bob1"
echo "BOB_SECOND=$bob2"
echo "RND=$rnd"
