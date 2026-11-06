#!/usr/bin/env bash
# The M17.1 check's site: the public site, which setup.sh built for this
# address and left in tester's home, served on loopback. Its server runs in
# the background with its PID in ~/site.pid, until the next run's prep.
set -euo pipefail
cd "$HOME"
port=18313
[[ -f site/docs/install/index.html ]] || { echo "no site in ~/site: run setup.sh first" >&2; exit 1; }
if [[ -f site.pid ]]; then
    kill "$(cat site.pid)" 2>/dev/null || true
fi
nohup python3 -m http.server "$port" --bind 127.0.0.1 --directory "$HOME/site" >site.log 2>&1 &
echo $! >site.pid
served() { curl -fsS "http://127.0.0.1:$port/docs/install/" 2>/dev/null | cmp -s - site/docs/install/index.html; }
for _ in $(seq 50); do
    served && break
    sleep 0.2
done
# Whatever else holds the port would have the check walk another site.
if ! kill -0 "$(cat site.pid)" 2>/dev/null || ! served; then
    echo "the site's server didn't start, or another serves its port: $(cat site.log)" >&2
    exit 1
fi
echo "SITE=http://127.0.0.1:$port"
