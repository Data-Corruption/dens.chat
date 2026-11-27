#!/usr/bin/env bash
# The collections check's den: alice owns the den, bob is a member; text
# channels general and role-gated raids (role Raiders assigned to bob),
# a DM between alice and bob with a photo message, and bob's Notes to Self.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh

general=$(channel general)
raids_role=$(api alice POST "/api/dens/$DEN/roles" '{"name":"Raiders"}' | json 'd["id"]')
bob_id=$(api bob GET "/api/dens/$DEN/state" | json 'd["me"]["id"]')
api alice PUT "/api/dens/$DEN/members/$bob_id/roles" "{\"role_ids\":[\"$raids_role\"]}" >/dev/null
api alice POST "/api/dens/$DEN/channels" "{\"name\":\"raids\",\"kind\":\"text\",\"role_ids\":[\"$raids_role\"]}" >/dev/null
raids=""
for _ in $(seq 80); do
    raids=$(api bob GET "/api/dens/$DEN/state" | json "next((c['id'] for c in d['channels'] if c['name'] == 'raids'), '')")
    [[ -n $raids ]] && break
    sleep 0.25
done
[[ -n $raids ]] || { echo "bob never saw #raids" >&2; exit 1; }

gen_msg_alice=$(send alice "$general" "Alice general guide")
gen_msg_bob=$(send bob "$general" "Bob tip for everyone")
raid_msg=$(send alice "$raids" "Secret raid tactic")

dm=$(open_dm alice bob)
dm_photo=$(upload alice photo.jpg beach.jpg "$dm")
dm_msg=$(send alice "$dm" "DM beach photo" "$dm_photo")

notes=$(api bob POST "/api/dens/$DEN/notes" | json 'd["id"]')
note_msg=$(send bob "$notes" "Bob private note")

echo "DEN=$DEN"
echo "GENERAL=$general"
echo "RAIDS=$raids"
echo "RAIDS_ROLE=$raids_role"
echo "BOB_ID=$bob_id"
echo "DM=$dm"
echo "NOTES=$notes"
echo "GEN_MSG_ALICE=$gen_msg_alice"
echo "GEN_MSG_BOB=$gen_msg_bob"
echo "RAID_MSG=$raid_msg"
echo "DM_MSG=$dm_msg"
echo "NOTE_MSG=$note_msg"
