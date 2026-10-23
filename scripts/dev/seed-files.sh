#!/usr/bin/env bash

# Fills a development den with files, for testing backups and restores of a
# large den by hand: COUNT files of MIB MiB each, one to a message. The
# files are zeros, which the den seals as it stores them, so what lands on
# its disk doesn't compress, as members' files don't. It raises the den's
# limits to take them first.
#
# Usage: scripts/dev/seed-files.sh INSTANCE [COUNT] [MIB] [CHANNEL]
#
# The instance must be a running development instance that hosts a den
# (service run --den-port) with at least one text channel, and the den's
# disk needs room for COUNT times MIB MiB, and a gigabyte. CHANNEL names
# the channel to fill, and defaults to the first. The script pairs itself
# like a browser would, through dens open --print.

set -euo pipefail

instance=${1:?usage: seed-files.sh INSTANCE [COUNT] [MIB] [CHANNEL]}
count=${2:-10}
mib=${3:-1024}
name=${4:-}
(( mib >= 1 && mib <= 1024 )) || { echo "error: a den takes files of 1 to 1024 MiB" >&2; exit 2; }
cd "$(dirname "$0")/../.."
bin=out/linux-amd64
[[ -x "$bin" ]] || { echo "error: build a development binary first: ./scripts/build.sh" >&2; exit 1; }
jar=$(mktemp)
file=$(mktemp)
trap 'rm -f "$jar" "$file"' EXIT
# Sparse, so it takes no room here, and curl knows its length to send.
truncate -s "$((mib << 20))" "$file"

url=$("$bin" open --print --instance "$instance")
base=${url%%/#*}
base=${base%/}
api() {
  curl -sS --fail-with-body -b "$jar" -c "$jar" -X "$1" -H "Origin: $base" -H "Content-Type: application/json" ${3:+-d "$3"} "$base$2"
}
api POST /api/pair "{\"token\":\"${url#*#token=}\"}" >/dev/null

den=$(api GET /api/dens | python3 -c 'import json, sys; print(next(d["den_id"] for d in json.load(sys.stdin)["dens"] if d["own"]))')
channel=$(api GET "/api/dens/$den/state" | NAME=$name python3 -c 'import json, os, sys
name = os.environ["NAME"]
print(next(c["id"] for c in json.load(sys.stdin)["channels"] if c["kind"] == "text" and name in ("", c["name"])))')

# Files of this size, and room for all of them, the member's and the den's.
api POST "/api/dens/$den/settings" "{\"limits\":{\"file_size\":$((mib << 20)),\"member_storage\":$((1 << 50)),\"den_storage\":$((1 << 50))}}" >/dev/null

nonce() { head -c 16 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n'; }
for i in $(seq 1 "$count"); do
  id=$(curl -sS --fail-with-body -b "$jar" -X POST -T "$file" -H "Origin: $base" -H "Content-Type: application/octet-stream" \
    -H "Dens-Filename: seed-$i.bin" "$base/api/dens/$den/uploads?channel=$channel" |
    python3 -c 'import json, sys; print(json.load(sys.stdin)["id"])')
  api POST "/api/dens/$den/channels/$channel/messages" "{\"nonce\":\"$(nonce)\",\"text\":\"Seed file $i of $count\",\"attachments\":[\"$id\"]}" >/dev/null
  printf 'Sent %d of %d\n' "$i" "$count"
done
echo "Seeded $count files of $mib MiB. Back the den up with: $bin backup --instance $instance"
