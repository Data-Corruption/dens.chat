#!/usr/bin/env bash
# The phone check's den (M13): alice's install hosts it; bob and carol
# join; dave's, which the driver starts new for each browser, shows the
# screens before a den. Its soundboard holds a sound (M14.3), and it has
# an emoji of its own (M15.2). general
# has a description and every kind of message; random a few more; Projects
# holds a channel with a long name and a voice channel; staff is staff
# only. Alice and bob share a DM, which opens at once (M16.1), where alice
# started over, so bob's page shows her keys changed.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
stop m2d
fresh
join carol
speak
general=$(channel general)
random=$(channel random)
projects=$(group Projects)
channel design-notes text "$projects" >/dev/null
channel the-longest-name-a-channel-takes text "$projects" >/dev/null
channel Standup voice "$projects" >/dev/null
api alice POST "/api/dens/$DEN/channels" '{"name":"staff","kind":"text","staff_only":true}' >/dev/null
api alice PATCH "/api/dens/$DEN/channels/$general" '{"description":"Anything goes here: say hi, share what you are up to, and **be kind**. The rules are in #design-notes.\nA second line, which only shows once the description is opened.\nAnd a third, with a link: https://dens.chat/docs/"}' >/dev/null
# Two may share at once, so the call's member shares beside the probe
# (M13.4).
limits=$(api alice GET "/api/dens/$DEN/state" | python3 -c 'import json, sys; d = json.load(sys.stdin)["call_limits"]; d["shares"] = 2; print(json.dumps({"call_limits": d}))')
api alice POST "/api/dens/$DEN/settings" "$limits" >/dev/null
./node browser/make-sound.mjs ping.ogg 1
api alice POST "/api/dens/$DEN/soundboard" "{\"name\":\"Ping\",\"upload\":\"$(upload alice ping.ogg ping.ogg)\"}" >/dev/null
# An emoji of the den's own (M15.2), which the phone's picker and
# completion offer.
./node browser/make-emoji.mjs party.png 96 96
api alice POST "/api/dens/$DEN/emoji" "{\"name\":\"party\",\"upload\":\"$(upload alice party.png party.png)\"}" >/dev/null

say() { send "$@" >/dev/null; }
printf 'A file to attach.\n' >note.txt
long=$(python3 -c 'print("\\n".join(f"Line {i} of a long message that folds, with some words to wrap it on a phone." for i in range(1, 25)))')
say alice "$general" "Welcome to **Voice Den**! Say hi, read the rules in #design-notes, and be kind."
say bob "$general" "hi all 👋"
say carol "$general" "Morning. Who's up for a call later? I'll be in Lounge around 8."
first=$(send bob "$general" "Bob here: can someone check the build?")
say alice "$general" "$long"
say bob "$general" "\`\`\`go\nfunc main() {\n\tfmt.Println(\\\"a line of code that runs well past the edge of a phone's screen, as code does\\\")\n}\n\`\`\`"
say carol "$general" "Here's the doc: https://example.com/a/very/long/path/that/goes/on/and/on/and/on?with=parameters&and=more"
say bob "$general" "@alice can you check these?\n[ ] buy milk\n[x] walk the dog\n[ ] a task with a description long enough to wrap onto another line on a phone"
say alice "$general" "> a quoted line from earlier\nand **bold**, _italic_, ~~struck~~, \`inline code\` and ||a spoiler||"
say alice "$general" "Link to Bob's message: $DEN_URL/#m/$DEN/$general/$first"
say bob "$general" "the view from here" "$(upload bob photo.jpg view.jpg)"
say alice "$general" "two from my phone" "$(upload alice photos/iphone-5.jpg one.jpg)" "$(upload alice photos/pixel-2.jpg two.jpg)"
say carol "$general" "a clip" "$(upload carol clip.mp4 clip.mp4)"
say bob "$general" "notes attached" "$(upload bob note.txt a-file-with-a-rather-long-name-for-a-phone.txt)"
say carol "$general" "Supercalifragilisticexpialidociousandthensomemorelettersthatneverbreakanywhereatall"
say carol "$general" "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
say bob "$general" "party time :party:"
say bob "$general" "Bob's last word"
for i in 1 2 3; do say alice "$random" "random $i"; done

dm=$(open_dm alice bob)
say alice "$dm" "a DM for bob, end to end"
say bob "$dm" "and one back"
# Alice starts over, as on a new phone, so the DM shows where her keys
# changed (M13.4, M16.1).
api alice POST "/api/dens/$DEN/seal/start-over" '{"password":"den password"}' >/dev/null
open_dm alice bob >/dev/null
say alice "$dm" "and one after starting over"

echo "DEN=$DEN"
echo "GENERAL=$general"
echo "RANDOM_ID=$random"
echo "LOUNGE=$LOUNGE"
echo "DM=$dm"
