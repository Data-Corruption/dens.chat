#!/usr/bin/env bash
# The emulator's den (M12): a development den in the browser checks'
# container (dens-browsers), behind the pinned Caddy with its internal
# certificate authority, at that container's address on Incus's bridge, as
# a den on a LAN would be. Its owner, desk, is the desktop: an instance on
# page port 18584, which Chromium there can drive. The container needs
# scripts/test/browser/setup.sh first, for its development binary.
#
# Usage: scripts/test/android/den.sh up       start Caddy and the den, made once
#        scripts/test/android/den.sh invite   print a one-use invite for the phone
#        scripts/test/android/den.sh root     print Caddy's root certificate
#        scripts/test/android/den.sh down     stop both
#        scripts/test/android/den.sh general  print the den's general channel's ID
#        scripts/test/android/den.sh channel NAME  print a text channel's ID, made if it isn't there
#        scripts/test/android/den.sh api METHOD PATH [JSON]   desk's page API
#        scripts/test/android/den.sh say CHANNEL TEXT [IDS]   a message from desk, with uploads by ID
#        scripts/test/android/den.sh upload FILE NAME         an upload from desk, in its home
#        scripts/test/android/den.sh dm MEMBER                desk's DM with a member by ID, its key started
#
# The app's Dens trusts the root through SSL_CERT_FILE, which phone.sh trust
# gives a development build.

set -euo pipefail
cd "$(dirname "$0")/../../.."
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
mkdir -p android-den
IP=$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1)
URL=https://$IP:8443
PORT=18584
JAR=$HOME/android-den/jar-sden
ROOT_CRT=$HOME/android-den/caddy/pki/authorities/local/root.crt

api() {
  local base=http://127.0.0.1:$PORT
  if [[ $# -ge 3 ]]; then
    curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $base" -H "Content-Type: application/json" --data "$3" "$base$2"
  else
    curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $base" "$base$2"
  fi
}
json() { python3 -c "import json, sys; d = json.load(sys.stdin); print($1)"; }
running() { [[ -f android-den/$1.pid ]] && kill -0 "$(cat "android-den/$1.pid")" 2>/dev/null; }
stop() {
  running "$1" || return 0
  local pid
  pid=$(cat "android-den/$1.pid")
  kill "$pid"
  while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
  rm -f "android-den/$1.pid"
}
den() { api GET /api/dens | json 'next(x["den_id"] for x in d["dens"] if x["own"])'; }

up() {
  cat >android-den/Caddyfile <<EOF
{
	admin off
	auto_https disable_redirects
	http_port 18580
	skip_install_trust
	storage file_system $HOME/android-den/caddy
}
$URL {
	tls internal
	reverse_proxy 127.0.0.1:18585
}
EOF
  if ! running caddy; then
    nohup ./caddy run --config android-den/Caddyfile --adapter caddyfile >android-den/caddy.log 2>&1 &
    echo $! >android-den/caddy.pid
  fi
  for _ in $(seq 50); do [[ -s $ROOT_CRT ]] && break; sleep 0.2; done
  if ! running sden; then
    SSL_CERT_FILE=$ROOT_CRT nohup ./dens-dev -i sden service run --port $PORT --den-port 18585 \
      --media-udp-port 17981 --media-tcp-port 17982 >android-den/sden.log 2>&1 &
    echo $! >android-den/sden.pid
  fi
  for _ in $(seq 100); do curl -s -o /dev/null "http://127.0.0.1:$PORT/api/status" && break; sleep 0.1; done
  if ! [[ -s $JAR ]]; then
    local link
    link=$(./dens-dev -i sden open --print)
    api POST /api/pair "{\"token\":\"${link#*#token=}\"}" >/dev/null
  fi
  if api GET /api/status | json 'd["passwordSet"]' | grep -q False; then
    api POST /api/password '{"password":"local password"}' >/dev/null
  fi
  if ! den >/dev/null 2>&1; then
    api POST /api/den "{\"name\":\"Android Den\",\"url\":\"$URL\",\"username\":\"desk\",\"display_name\":\"Desk\",\"password\":\"den password\"}" >/dev/null
    api POST "/api/dens/$(den)/channels" '{"name":"Lounge","kind":"voice"}' >/dev/null
  fi
  if ! api GET "/api/dens/$(den)/state" | json "any(c['name'] == 'general' for c in d['channels'])" | grep -q True; then
    api POST "/api/dens/$(den)/channels" '{"name":"general","kind":"text"}' >/dev/null
  fi
  echo "Android Den at $URL, owned by desk on page port $PORT"
}

case $1 in
  up) up ;;
  api) shift; api "$@" ;;
  den) den ;;
  general) api GET "/api/dens/$(den)/state" | json "next(c['id'] for c in d['channels'] if c['name'] == 'general')" ;;
  channel)
    name=$2
    named() { api GET "/api/dens/$(den)/state" | json "next((c['id'] for c in d['channels'] if c['name'] == '$name'), '')"; }
    [[ -n $(named) ]] || api POST "/api/dens/$(den)/channels" "{\"name\":\"$name\",\"kind\":\"text\"}" >/dev/null
    for _ in $(seq 40); do
      id=$(named)
      [[ -n $id ]] && { echo "$id"; exit 0; }
      sleep 0.25
    done
    echo "no channel $name" >&2
    exit 1
    ;;
  say) api POST "/api/dens/$(den)/channels/$2/messages" "{\"nonce\":\"$(python3 -c 'import secrets; print(secrets.token_urlsafe(16))')\",\"text\":$(python3 -c 'import json, sys; print(json.dumps(sys.argv[1]))' "$3"),\"attachments\":[${4:-}]}" | json 'd["id"]' ;;
  upload) curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X POST -H "Origin: http://127.0.0.1:$PORT" -H "Content-Type: application/octet-stream" \
      -H "Dens-Filename: $3" --data-binary "@$2" "http://127.0.0.1:$PORT/api/dens/$(den)/uploads" | json 'd["id"]' ;;
  invite) api POST "/api/dens/$(den)/invites" '{"expires_in":86400,"max_uses":1}' | json 'd["invite"]' ;;
  dm)
    dm=$(api POST "/api/dens/$(den)/dms" "{\"member_id\":\"$2\"}" | json 'd["id"]')
    if [[ $(api GET "/api/dens/$(den)/dms/$dm/check" | json 'd["stage"]') == none ]]; then
      api POST "/api/dens/$(den)/dms/$dm/key" '{}' >/dev/null
    fi
    echo "$dm"
    ;;
  root) cat "$ROOT_CRT" ;;
  down) stop sden; stop caddy ;;
esac
GUEST
  incus file push -q --uid 1000 --gid 1000 --mode 0755 "$tmp" "$BROWSERS/home/tester/android-den.sh"
  rm -f "$tmp"
}

case ${1:-} in
  up)
    as_tester test -x caddy || incus file push -q --uid 1000 --gid 1000 --mode 0755 "$(./scripts/vendor.sh caddy | sed -n 's/^caddy=//p')" "$BROWSERS/home/tester/caddy"
    guest
    as_tester ./android-den.sh up
    ;;
  invite | root | down | api | den | general | channel | say | upload | dm)
    guest
    as_tester ./android-den.sh "$@"
    ;;
  *)
    sed -n '9,18p' "$0" >&2
    exit 2
    ;;
esac
