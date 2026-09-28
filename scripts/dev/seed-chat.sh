#!/usr/bin/env bash

# Fills a development den's first text channel with messages, for testing
# the message list: an anchor message, COUNT filler messages, then a reply
# to the anchor, so clicking the reply jumps COUNT messages back.
#
# Usage: scripts/dev/seed-chat.sh INSTANCE [COUNT]
#
# The instance must be a running development instance that hosts a den
# (service run --den-port) with at least one text channel. The script pairs
# itself like a browser would, through dens open --print. Development
# builds relax the den's rate limits, which is what makes this quick.

set -euo pipefail

instance=${1:?usage: seed-chat.sh INSTANCE [COUNT]}
count=${2:-5000}
cd "$(dirname "$0")/../.."
bin=out/linux-amd64
[[ -x "$bin" ]] || { echo "error: build a development binary first: ./scripts/build.sh" >&2; exit 1; }
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT

url=$("$bin" open --print --instance "$instance")
base=${url%%/#*}
base=${base%/}
api() {
  curl -sS --fail-with-body -b "$jar" -c "$jar" -X "$1" -H "Origin: $base" -H "Content-Type: application/json" ${3:+-d "$3"} "$base$2"
}
api POST /api/pair "{\"token\":\"${url#*#token=}\"}" >/dev/null

den=$(api GET /api/dens | python3 -c 'import json, sys; print(next(d["den_id"] for d in json.load(sys.stdin)["dens"] if d["own"]))')
channel=$(api GET "/api/dens/$den/state" | python3 -c 'import json, sys; print(next(c["id"] for c in json.load(sys.stdin)["channels"] if c["kind"] == "text"))')

nonce() { head -c 16 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n'; }
send() { # TEXT [REPLY_TO] prints the new message's ID
  local body
  body="{\"nonce\":\"$(nonce)\",\"text\":\"$1\"${2:+,\"reply_to\":\"$2\"}}"
  api POST "/api/dens/$den/channels/$channel/messages" "$body" | python3 -c 'import json, sys; print(json.load(sys.stdin)["id"])'
}

anchor=$(send "Anchor: a reply to this comes $count messages later.")
for i in $(seq 1 "$count"); do
  api POST "/api/dens/$den/channels/$channel/messages" "{\"nonce\":\"$(nonce)\",\"text\":\"Filler message $i of $count\"}" >/dev/null
  (( i % 500 == 0 )) && printf 'Sent %d of %d\n' "$i" "$count"
done
send "This replies to the anchor $count messages back. Click the quote above to jump there." "$anchor" >/dev/null
echo "Seeded $count messages. Open the channel and click the last message's quote."
