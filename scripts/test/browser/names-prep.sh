#!/usr/bin/env bash
# The M19.5 check's den: alice's install hosts it, with general, where
# alice, bob and carol have each written and bob has replied to alice, and
# a DM between alice and bob. Carol's name is Cyrillic, styled in a font
# that has none of it.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
join carol
general=$(channel general)
api carol PATCH "/api/dens/$DEN/me" '{"display_name":"Карина","name_style":{"font":"bangers","color":"#2f9e44"}}' >/dev/null
first=$(send alice "$general" "Alice's first message.")
second=$(send bob "$general" "Bob's message.")
send carol "$general" "Carol's message." >/dev/null
reply=$(api bob POST "/api/dens/$DEN/channels/$general/messages" "{\"nonce\":\"$(nonce)\",\"text\":\"Bob's reply.\",\"reply_to\":\"$first\"}" | json 'd["id"]')
dm=$(open_dm alice bob)
send alice "$dm" "A message between the two." >/dev/null
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
echo "GENERAL=$general"
echo "DM=$dm"
echo "FIRST=$first"
echo "SECOND=$second"
echo "REPLY=$reply"
