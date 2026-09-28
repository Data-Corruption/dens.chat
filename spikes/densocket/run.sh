#!/usr/bin/env bash
# Runs the densocket spike: local compression and memory measurements, then
# a den behind Caddy (tls internal) in an Incus container, followed from the
# host through a Caddy reload, a Caddy restart and a den restart.
# Results land in out/spikes/densocket/<run>/.
set -euo pipefail
exec </dev/null
cd "$(dirname "$0")/.."
repo=$(cd .. && pwd)
run="$repo/out/spikes/densocket/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run"
bin="$run/densocket"
CGO_ENABLED=0 go build -o "$bin" ./densocket

echo "== local measurements"
"$bin" server -listen 127.0.0.1:18599 > "$run/local-server.log" 2>&1 &
srv=$!
trap 'kill $srv 2>/dev/null || :' EXIT
until curl -s -o /dev/null http://127.0.0.1:18599/stats; do sleep 0.1; done
"$bin" measure -url ws://127.0.0.1:18599/ws | tee "$run/measure.txt"
"$bin" encodings | tee "$run/encodings.txt"
"$bin" connmem -url ws://127.0.0.1:18599/ws -stats http://127.0.0.1:18599/stats -n 200 | tee "$run/connmem.txt"
kill $srv

echo "== den behind Caddy"
name="densocket-spike-$$"
trap 'incus delete --force "$name" >/dev/null 2>&1 || :' EXIT
incus init -q dens-e2e/debian-13 "$name" -c security.nesting=true >/dev/null
incus start "$name"
until incus exec "$name" -- sh -c 'ip route show default | grep -q .' 2>/dev/null; do sleep 0.25; done
incus exec "$name" -- sh -c 'apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq caddy >/dev/null'
incus file push -q "$bin" "$name/usr/local/bin/densocket"
incus exec "$name" -- sh -c '
cat > /etc/systemd/system/densrv.service <<EOF
[Service]
ExecStart=/usr/local/bin/densocket server -listen 127.0.0.1:8485
EOF
cat > /etc/caddy/Caddyfile <<EOF
den.test {
	tls internal
	reverse_proxy 127.0.0.1:8485
}
EOF
systemctl daemon-reload
systemctl enable --now densrv
systemctl restart caddy'
ip=$(incus list "$name" --format csv -c 4 | awk '{print $1}')
root=/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt
until incus exec "$name" -- test -f "$root"; do sleep 0.25; done
incus file pull -q "$name$root" - > "$run/caddy-root.crt"
until [[ "$(curl -s -o /dev/null -w '%{http_code}' --cacert "$run/caddy-root.crt" --resolve "den.test:443:$ip" https://den.test/ws)" == 401 ]]; do sleep 0.25; done
incus exec "$name" -- caddy version | tee "$run/caddy-version.txt"

"$bin" probe -url wss://den.test/ws -ca "$run/caddy-root.crt" -connect "$ip:443" | tee "$run/probe.txt"

(
  sleep 5; echo ">> caddy reload"; incus exec "$name" -- systemctl reload caddy
  sleep 10; echo ">> caddy restart"; incus exec "$name" -- systemctl restart caddy
  sleep 10; echo ">> den restart"; incus exec "$name" -- systemctl restart densrv
) &
"$bin" follow -url wss://den.test/ws -ca "$run/caddy-root.crt" -connect "$ip:443" -for 40s | tee "$run/follow.txt"
wait
echo "Results: $run"
