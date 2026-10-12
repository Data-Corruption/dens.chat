#!/usr/bin/env bash
# The M8 hostile-text check's den: a channel of 200 messages from alice,
# each 4,000 characters, a message's most, of markdown made to be slow to
# parse, as any member could send.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
hostile=$(channel hostile)
python3 - >hostile.jsonl <<'PY'
import base64, json, os
units = ["* ", "** ", "_ ", "|| ", "~~ ", "*_~~|| ", "*`", "ahttp://", "@" + "a" * 40 + " "]
for i in range(200):
    if i % 10 == 9:
        text = "http://a" + ")" * 3992
    else:
        unit = units[i % len(units)]
        text = (unit * (4000 // len(unit) + 1))[:4000]
    nonce = base64.urlsafe_b64encode(os.urandom(16)).decode().rstrip("=")
    print(json.dumps({"nonce": nonce, "text": text}))
PY
while read -r body; do
    api alice POST "/api/dens/$DEN/channels/$hostile/messages" "$body" >/dev/null
done <hostile.jsonl
echo "DEN=$DEN"
echo "HOSTILE=$hostile"
