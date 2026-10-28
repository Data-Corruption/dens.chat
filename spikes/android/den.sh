#!/usr/bin/env bash
# The emulator's den: a development den in the browser checks' container
# (dens-browsers), behind the pinned Caddy with its internal certificate
# authority, at that container's address on Incus's bridge, as a den on a
# LAN would be. Its owner, desk, is the desktop in the emulator's calls:
# an instance on page port 18584, which Chromium there drives.
#
# Usage: spikes/android/den.sh up       start Caddy and the den, made once
#        spikes/android/den.sh invite   print a one-use invite for the phone
#        spikes/android/den.sh root     print Caddy's root certificate
#        spikes/android/den.sh down     stop both
#        spikes/android/den.sh api METHOD PATH [JSON]   desk's page API
#        spikes/android/den.sh say CHANNEL TEXT [IDS]   a message from desk, with uploads by ID
#        spikes/android/den.sh upload FILE NAME         an upload from desk, in its home
#
# The phone's Dens trusts the root through SSL_CERT_FILE, which emulator.sh
# gives it.

set -euo pipefail
cd "$(dirname "$0")/../.."
BROWSERS=${BROWSERS:-dens-browsers}

as_tester() {
  incus exec "$BROWSERS" --user 1000 --group 1000 --cwd /home/tester \
    --env HOME=/home/tester --env XDG_RUNTIME_DIR=/run/user/1000 -- "$@"
}

guest() {
  local tmp
  tmp=$(mktemp)
  cat >"$tmp" <<'GUEST'
#!/usr/bin/env bash
set -euo pipefail
cd "$HOME"
mkdir -p spike
IP=$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1)
URL=https://$IP:8443
PORT=18584
JAR=$HOME/spike/jar-sden
ROOT_CRT=$HOME/spike/caddy/pki/authorities/local/root.crt

api() {
  local base=http://127.0.0.1:$PORT
  if [[ $# -ge 3 ]]; then
    curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $base" -H "Content-Type: application/json" --data "$3" "$base$2"
  else
    curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $base" "$base$2"
  fi
}
json() { python3 -c "import json, sys; d = json.load(sys.stdin); print($1)"; }
running() { [[ -f spike/$1.pid ]] && kill -0 "$(cat "spike/$1.pid")" 2>/dev/null; }
stop() {
  running "$1" || return 0
  local pid
  pid=$(cat "spike/$1.pid")
  kill "$pid"
  while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
  rm -f "spike/$1.pid"
}
den() { api GET /api/dens | json 'next(x["den_id"] for x in d["dens"] if x["own"])'; }

up() {
  cat >spike/Caddyfile <<EOF
{
	admin off
	auto_https disable_redirects
	http_port 18580
	skip_install_trust
	storage file_system $HOME/spike/caddy
}
$URL {
	tls internal
	reverse_proxy 127.0.0.1:18585
}
EOF
  if ! running caddy; then
    nohup ./caddy run --config spike/Caddyfile --adapter caddyfile >spike/caddy.log 2>&1 &
    echo $! >spike/caddy.pid
  fi
  for _ in $(seq 50); do [[ -s $ROOT_CRT ]] && break; sleep 0.2; done
  if ! running sden; then
    SSL_CERT_FILE=$ROOT_CRT nohup ./dens-dev -i sden service run --port $PORT --den-port 18585 \
      --media-udp-port 17981 --media-tcp-port 17982 >spike/sden.log 2>&1 &
    echo $! >spike/sden.pid
  fi
  for _ in $(seq 100); do curl -s -o /dev/null "http://127.0.0.1:$PORT/api/status" && break; sleep 0.1; done
  if ! [[ -s $JAR ]]; then
    local link
    link=$(./dens-dev -i sden open --print)
    api POST /api/pair "{\"token\":\"${link#*#token=}\"}" >/dev/null
    api POST /api/password '{"password":"local password"}' >/dev/null
  fi
  if ! den >/dev/null 2>&1; then
    api POST /api/den "{\"name\":\"Spike Den\",\"url\":\"$URL\",\"username\":\"desk\",\"display_name\":\"Desk\",\"password\":\"den password\"}" >/dev/null
    api POST "/api/dens/$(den)/channels" '{"name":"Lounge","kind":"voice"}' >/dev/null
  fi
  if ! api GET "/api/dens/$(den)/state" | json "any(c['name'] == 'general' for c in d['channels'])" | grep -q True; then
    api POST "/api/dens/$(den)/channels" '{"name":"general","kind":"text"}' >/dev/null
  fi
  echo "Spike Den at $URL, owned by desk on page port $PORT"
}

case $1 in
  up) up ;;
  api) shift; api "$@" ;;
  den) den ;;
  say) api POST "/api/dens/$(den)/channels/$2/messages" "{\"nonce\":\"$(python3 -c 'import secrets; print(secrets.token_urlsafe(16))')\",\"text\":$(python3 -c 'import json, sys; print(json.dumps(sys.argv[1]))' "$3"),\"attachments\":[$4]}" | json 'd["id"]' ;;
  upload) curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X POST -H "Origin: http://127.0.0.1:$PORT" -H "Content-Type: application/octet-stream" \
      -H "Dens-Filename: $3" --data-binary "@$2" "http://127.0.0.1:$PORT/api/dens/$(den)/uploads" | json 'd["id"]' ;;
  invite) api POST "/api/dens/$(den)/invites" '{"expires_in":86400,"max_uses":1}' | json 'd["invite"]' ;;
  root) cat "$ROOT_CRT" ;;
  down) stop sden; stop caddy ;;
esac
GUEST
  incus file push -q --uid 1000 --gid 1000 --mode 0755 "$tmp" "$BROWSERS/home/tester/spike-den.sh"
  rm -f "$tmp"
}

case ${1:-} in
  up)
    as_tester test -x caddy || incus file push -q --uid 1000 --gid 1000 --mode 0755 "$(./scripts/vendor.sh caddy | sed -n 's/^caddy=//p')" "$BROWSERS/home/tester/caddy"
    guest
    as_tester ./spike-den.sh up
    ;;
  invite | root | down | api | den | say | upload)
    guest
    as_tester ./spike-den.sh "$@"
    ;;
  *)
    sed -n '9,15p' "$0" >&2
    exit 2
    ;;
esac
