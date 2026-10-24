#!/usr/bin/env bash
# The M10 move check's dens: alice's instance hosts hers at the den's
# address, 18490, which forward.mjs sends to her den listener, as Caddy and
# the router would, with bob in it; carol's, hosting on ports of its own,
# stands in for the server the driver restores the den's export into. The
# forward sends the address to the port ~/forward-to names, and the
# browsers download to ~/downloads.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
for name in m2a m2b m2c forward; do stop "$name"; done
rm -rf "$HOME/.local/share/dens-dev"/m2? "$HOME"/jar-m2? "$HOME/downloads"
mkdir -p "$HOME/downloads"
echo 18485 >"$HOME/forward-to"
nohup ./node browser/forward.mjs 18490 "$HOME/forward-to" >forward.log 2>&1 &
echo $! >forward.pid
start_den
start m2b --port 18486
pair alice
api alice POST /api/den '{"name":"Moving Den","url":"http://127.0.0.1:18490","username":"alice","display_name":"Alice","password":"den password"}' >/dev/null
DEN=$(api alice GET /api/dens | json 'next(x["den_id"] for x in d["dens"] if x["own"])')
join bob
send alice "$(channel general)" "before the move" >/dev/null
start m2c --port 18487 --den-port 18488 --media-udp-port 17883 --media-tcp-port 17884
pair carol
echo "DEN=$DEN"
