#!/usr/bin/env bash
# The M8 answers check's den, behind den-proxy.mjs, which logs each call
# answer a client sends it to ~/answers.jsonl: its address is the proxy's,
# so the members' clients reach it there. Carol joins, whom the voice probe
# plays; Firefox's microphone plays speech.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
stop den-proxy
rm -f answers.jsonl
nohup ./node browser/den-proxy.mjs 18495 18485 answers.jsonl >den-proxy.log 2>&1 &
echo $! >den-proxy.pid
DEN_URL=http://127.0.0.1:18495
fresh
join carol
speak
echo "DEN=$DEN"
echo "LOUNGE=$LOUNGE"
