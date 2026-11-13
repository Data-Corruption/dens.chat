#!/usr/bin/env bash
# The Relay spike's runs in two Incus containers on one bridge (README.md,
# "Here"): dens-browsers, side a, and dens-relay, a Fedora 44 made for
# this, side b. WSL's kernel can't load firewalld's own rules, so each
# firewall is the rule firewalld and NixOS come down to for a port nobody
# opened: replies come in, anything else is dropped (NixOS) or rejected
# with an ICMP error (firewalld).
#
# Usage: spikes/relay/run/here.sh COMMAND
#   setup            build the probe and push it to both containers
#   fwcheck          each firewall, tried with a packet nobody asked for
#   firewalls        the link through each pair of firewalls, each start order
#   sandbox          side b under the installed service's unit settings
#   gone             how soon each side knows: the peer killed, then the path cut
#   capture          question 4's exercise against the container's PulseAudio
#   clicks           question 6 in software: a click's delay from the source's capture to the receiver's output
#   blips            short cuts in the path, as Wi-Fi gives, with SCTP's timer as it is and capped
#   sound [SECONDS]  tone and keys across for SECONDS, 600 unless given
#   rtp              the sound as RTP against the sound on its channel: what each costs
#                    the processor, short cuts, and packets lost at random
#
# Logs land in spikes/relay/logs/here/.

set -euo pipefail
cd "$(dirname "$0")/../.."
A=dens-browsers
B=dens-relay
PORT=47800
LOGS=relay/logs/here
mkdir -p "$LOGS"

ip_of() { incus exec "$1" -- ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1; }
IPA=$(ip_of $A)
IPB=$(ip_of $B)

# fw CONTAINER none|drop|reject: the firewall on the probe's port.
fw() {
  local mode=$2
  incus exec "$1" -- sh -c '
    nft delete table inet relayfw 2>/dev/null || :
    iptables -D INPUT -j RELAYFW 2>/dev/null || :
    iptables -F RELAYFW 2>/dev/null || :
    iptables -X RELAYFW 2>/dev/null || :
    conntrack -F 2>/dev/null || :'
  [[ $mode == none ]] && return 0
  local verdict="drop"
  [[ $mode == reject ]] && verdict="reject with icmpx type admin-prohibited"
  if incus exec "$1" -- sh -c 'command -v nft >/dev/null || exit 1' 2>/dev/null; then
    incus exec "$1" -- nft -f - <<EOF
table inet relayfw {
  chain input {
    type filter hook input priority 0; policy accept;
    ct state established,related accept
    udp dport $PORT $verdict
  }
}
EOF
  else
    local target="DROP"
    [[ $mode == reject ]] && target="REJECT --reject-with icmp-admin-prohibited"
    incus exec "$1" -- sh -c "
      iptables -N RELAYFW
      iptables -A RELAYFW -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
      iptables -A RELAYFW -p udp --dport $PORT -j $target
      iptables -I INPUT -j RELAYFW"
  fi
}

# probe CONTAINER NAME ARGS...: one probe, in the background, its log NAME.
probe() {
  local c=$1 name=$2
  shift 2
  incus exec "$c" -- sh -c "rm -f /tmp/$name.log; exec /usr/local/bin/relayprobe -quiet -log /tmp/$name.log $*" &
}

fetch() {
  local c=$1 name=$2
  incus file pull -q "$c/tmp/$name.log" "$LOGS/$name.log"
}

# pair NAME FWA FWB DELAY_A DELAY_B SECONDS [ARGS_A] [ARGS_B]
pair() {
  local name=$1 fwa=$2 fwb=$3 da=$4 db=$5 secs=$6 argsa=${7:-} argsb=${8:-}
  fw $A "$fwa"
  fw $B "$fwb"
  (sleep "$da"; probe $A "$name-a" -peer "$IPB" -role a -page off -for "${secs}s" $argsa; wait) &
  (sleep "$db"; probe $B "$name-b" -peer "$IPA" -role b -page off -for "${secs}s" $argsb; wait) &
  wait
  sleep 1
  fetch $A "$name-a"
  fetch $B "$name-b"
  printf '%-28s a: %-34s b: %s\n' "$name" \
    "$(grep -m1 -o 'up after=[0-9.]*ms attempt=[0-9]*' "$LOGS/$name-a.log" || echo 'never up')" \
    "$(grep -m1 -o 'up after=[0-9.]*ms attempt=[0-9]*' "$LOGS/$name-b.log" || echo 'never up')"
}

case ${1:-} in
  setup)
    CGO_ENABLED=0 go build -trimpath -o relay/bin/relayprobe-linux-amd64 ./relay
    for c in $A $B; do
      incus file push -q relay/bin/relayprobe-linux-amd64 "$c/usr/local/bin/relayprobe.new"
      incus exec "$c" -- mv /usr/local/bin/relayprobe.new /usr/local/bin/relayprobe
    done
    incus exec $B -- sh -c 'command -v conntrack >/dev/null || dnf install -y -q conntrack-tools >/dev/null 2>&1 || :'
    echo "a: $A $IPA, b: $B $IPB"
    ;;
  fwcheck)
    # Each firewall, asked: does a packet nobody asked for reach the port?
    listen='import socket,sys
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM); s.bind(("0.0.0.0",47800)); s.settimeout(2)
try: s.recvfrom(100); print(sys.argv[1], "let it in")
except OSError: print(sys.argv[1], "kept it out")'
    send='import socket,sys
socket.socket(socket.AF_INET,socket.SOCK_DGRAM).sendto(b"hi",(sys.argv[1],47800))'
    for mode in none drop reject; do
      fw $A $mode
      fw $B $mode
      incus exec $A -- python3 -c "$listen" "a/$mode" &
      incus exec $B -- python3 -c "$listen" "b/$mode" &
      sleep 0.7
      incus exec $A -- python3 -c "$send" "$IPB"
      incus exec $B -- python3 -c "$send" "$IPA"
      wait
    done
    fw $A none
    fw $B none
    ;;
  firewalls)
    for fws in "none none" "drop drop" "reject reject" "drop none" "none drop" "reject drop" "drop reject"; do
      read -r fa fb <<<"$fws"
      pair "fw-$fa-$fb-together" "$fa" "$fb" 0 0 8
      pair "fw-$fa-$fb-a-first" "$fa" "$fb" 0 5 12
      pair "fw-$fa-$fb-b-first" "$fa" "$fb" 5 0 12
    done
    fw $A none
    fw $B none
    ;;
  sandbox)
    # Side b as Dens's unit runs its service (internal/install's unit
    # template), under an account of its own.
    fw $A drop
    fw $B drop
    incus exec $B -- rm -f /var/lib/relayprobe/sandbox-b.log
    probe $A sandbox-a -peer "$IPB" -role a -page off -for 12s -send tone -keys synth
    incus exec $B -- systemd-run --quiet --wait --collect --unit relayprobe-sandbox \
      -p DynamicUser=yes -p StateDirectory=relayprobe -p UMask=0077 -p LimitCORE=0 \
      -p NoNewPrivileges=yes -p ProtectSystem=strict -p ProtectHome=yes -p PrivateTmp=yes -p PrivateDevices=yes \
      -p ProtectKernelTunables=yes -p ProtectKernelModules=yes -p ProtectKernelLogs=yes -p ProtectControlGroups=yes \
      -p ProtectClock=yes -p ProtectHostname=yes -p ProtectProc=invisible -p ProcSubset=pid \
      -p RestrictNamespaces=yes -p RestrictRealtime=yes -p RestrictSUIDSGID=yes -p LockPersonality=yes \
      -p MemoryDenyWriteExecute=yes -p RemoveIPC=yes -p CapabilityBoundingSet= -p AmbientCapabilities= \
      -p "RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK" -p SystemCallArchitectures=native \
      -p SystemCallFilter=@system-service -p SystemCallErrorNumber=EPERM \
      /usr/local/bin/relayprobe -quiet -log /var/lib/relayprobe/sandbox-b.log -peer "$IPA" -role b -page 127.0.0.1:47880 -for 10s || echo "the unit failed"
    wait
    fetch $A sandbox-a
    incus file pull -q $B/var/lib/private/relayprobe/sandbox-b.log "$LOGS/sandbox-b.log" 2>/dev/null ||
      incus file pull -q $B/var/lib/relayprobe/sandbox-b.log "$LOGS/sandbox-b.log"
    grep -h -E 'where=|up after|sound rx|page  at' "$LOGS/sandbox-b.log" | cut -c1-220
    fw $A none
    fw $B none
    ;;
  gone)
    fw $A drop
    fw $B drop
    for ice in "5s,25s,2s" "1s,3s,500ms"; do
      tag=${ice//,/-}
      # The peer killed outright, a held key on its side.
      probe $A "gone-kill-$tag-a" -peer "$IPB" -role a -page off -for 45s -ice "$ice" -send tone
      probe $B "gone-kill-$tag-b" -peer "$IPA" -role b -page off -for 45s -ice "$ice" -keys synth:20s
      sleep 8
      incus exec $B -- pkill -9 -x relayprobe
      echo "$(date +%T.%3N) killed b" | tee "$LOGS/gone-kill-$tag.marks"
      sleep 34
      incus exec $A -- pkill -x relayprobe || :
      wait
      fetch $A "gone-kill-$tag-a"
      # The path cut for 12 s, as Wi-Fi dropping and coming back, then for good.
      probe $A "gone-cut-$tag-a" -peer "$IPB" -role a -page off -for 75s -ice "$ice" -send tone
      probe $B "gone-cut-$tag-b" -peer "$IPA" -role b -page off -for 75s -ice "$ice" -keys synth:20s
      sleep 8
      incus exec $B -- nft 'add table inet relaycut; add chain inet relaycut in { type filter hook input priority -10; policy accept; }; add chain inet relaycut out { type filter hook output priority -10; policy accept; }; add rule inet relaycut in udp dport '$PORT' drop; add rule inet relaycut out udp sport '$PORT' drop'
      echo "$(date +%T.%3N) cut" | tee "$LOGS/gone-cut-$tag.marks"
      sleep 12
      incus exec $B -- nft delete table inet relaycut
      echo "$(date +%T.%3N) restored" | tee -a "$LOGS/gone-cut-$tag.marks"
      sleep 10
      incus exec $B -- nft 'add table inet relaycut; add chain inet relaycut in { type filter hook input priority -10; policy accept; }; add chain inet relaycut out { type filter hook output priority -10; policy accept; }; add rule inet relaycut in udp dport '$PORT' drop; add rule inet relaycut out udp sport '$PORT' drop'
      echo "$(date +%T.%3N) cut again" | tee -a "$LOGS/gone-cut-$tag.marks"
      sleep 35
      incus exec $B -- nft delete table inet relaycut
      echo "$(date +%T.%3N) restored again" | tee -a "$LOGS/gone-cut-$tag.marks"
      wait
      fetch $A "gone-cut-$tag-a"
      fetch $B "gone-cut-$tag-b"
    done
    fw $A none
    fw $B none
    ;;
  capture)
    # Question 4's exercise against the container's PulseAudio, as tester.
    incus exec $A -- su - tester -c "rm -f /tmp/capture.log; XDG_RUNTIME_DIR=/run/user/1000 /usr/local/bin/relayprobe -quiet -send system -exercise -page off -for 125s -log /tmp/capture.log"
    fetch $A capture
    grep -E 'exer|event' "$LOGS/capture.log" | cut -c1-200
    ;;
  clicks)
    # Question 6 in software: both probes in one container over loopback.
    # The source captures the default output, where a browser plays the
    # click page; the receiver's page plays into a second output, whose
    # monitor the receiver listens to for each click coming out.
    cat >"$LOGS/clicks-inside.sh" <<'INSIDE'
set -u
export DISPLAY=:97 XDG_RUNTIME_DIR=/run/user/1000
Xvfb :97 -screen 0 1600x700x24 >/dev/null 2>&1 &
x=$!
sleep 1
openbox >/dev/null 2>&1 &
wm=$!
module=$(pactl load-module module-null-sink sink_name=relayout)
P=/usr/local/bin/relayprobe
$P -quiet -local 127.0.0.1 -port 47811 -peer 127.0.0.1:47812 -role a -send system -page off -for 75s -log /tmp/clicks-a.log &
$P -quiet -local 127.0.0.1 -port 47812 -peer 127.0.0.1:47811 -role b -monitor -source relayout.monitor -page 127.0.0.1:47890 -buffer "$1" -for 75s -log /tmp/clicks-b.log &
sleep 3
rm -rf /tmp/relay-click /tmp/relay-play
chromium --user-data-dir=/tmp/relay-click --no-first-run --no-sandbox --autoplay-policy=no-user-gesture-required \
  --window-position=0,0 --window-size=700,500 'http://127.0.0.1:47890/click.html?auto=1' >/dev/null 2>&1 &
c1=$!
PULSE_SINK=relayout chromium --user-data-dir=/tmp/relay-play --no-first-run --no-sandbox --autoplay-policy=no-user-gesture-required \
  --window-position=760,0 --window-size=700,500 'http://127.0.0.1:47890/?auto=1&name=clicks' >/dev/null 2>&1 &
c2=$!
sleep 74
kill $c1 $c2 2>/dev/null
sleep 2
pactl unload-module "$module"
kill $wm $x 2>/dev/null
INSIDE
    incus file push -q "$LOGS/clicks-inside.sh" $A/home/tester/relay-clicks.sh
    for buffer in 40 20; do
      incus exec $A -- rm -f /tmp/clicks-a.log /tmp/clicks-b.log
      incus exec $A -- su - tester -c "bash /home/tester/relay-clicks.sh $buffer"
      incus file pull -q $A/tmp/clicks-a.log "$LOGS/clicks-$buffer-a.log"
      incus file pull -q $A/tmp/clicks-b.log "$LOGS/clicks-$buffer-b.log"
      echo "buffer $buffer ms: delays from the source's capture to the receiver's output"
      grep -o 'delay=[0-9.]*ms' "$LOGS/clicks-$buffer-b.log" | tr -d 'a-z=' | sort -n | paste -sd' '
      relay/run/players-summary.sh "$LOGS/clicks-$buffer-b.log" || :
    done
    ;;
  blips)
    # Short cuts, as Wi-Fi gives: 200 ms every 5 s, then 1 s every 8 s, with
    # SCTP's timer as Pion has it and capped.
    fw $A drop
    fw $B drop
    cat >"$LOGS/blips-inside.sh" <<'INSIDE'
cut() {
  nft 'add table inet relaycut; add chain inet relaycut in { type filter hook input priority -10; policy accept; }; add chain inet relaycut out { type filter hook output priority -10; policy accept; }; add rule inet relaycut in udp dport 47800 drop; add rule inet relaycut out udp sport 47800 drop'
  sleep "$1"
  nft delete table inet relaycut
}
sleep 12
for _ in 1 2 3 4 5 6; do cut 0.2; sleep 5; done
for _ in 1 2 3; do cut 1; sleep 8; done
INSIDE
    incus file push -q "$LOGS/blips-inside.sh" $B/root/blips.sh
    for rto in ${2:-0 250ms}; do
      probe $A "blips-$rto-a" -peer "$IPB" -role a -page off -for 80s -send tone -rto "$rto"
      probe $B "blips-$rto-b" -peer "$IPA" -role b -page off -for 80s -send tone -rto "$rto"
      incus exec $B -- sh /root/blips.sh
      wait
      fetch $A "blips-$rto-a"
      fetch $B "blips-$rto-b"
      echo "rto=$rto: what a received each ten seconds, then what b sent"
      grep -h 'sound rx' "$LOGS/blips-$rto-a.log" | grep -oE 'packets=[0-9]+ lost=[0-9]+|gap-max=[0-9.]+ms' | paste -sd' ' | fold -w 150
      grep -h 'sound tx' "$LOGS/blips-$rto-b.log" | grep -oE 'packets=[0-9]+ dropped=[0-9]+|queued-max=[0-9]+B' | paste -sd' ' | fold -w 150
    done
    fw $A none
    fw $B none
    ;;
  sound)
    secs=${2:-600}
    fw $A drop
    fw $B drop
    probe $A sound-a -peer "$IPB" -role a -page off -for "${secs}s" -send tone -keys synth
    probe $B sound-b -peer "$IPA" -role b -page off -for "${secs}s" -send tone -keys synth
    wait
    fetch $A sound-a
    fetch $B sound-b
    fw $A none
    fw $B none
    ;;
  rtp)
    fw $A drop
    fw $B drop
    # ab NAME SECONDS ARGS: one run each way of carrying the sound.
    ab() {
      local name=$1 secs=$2 how
      shift 2
      for how in sctp rtp; do
        probe $A "$name-$how-a" -peer "$IPB" -role a -page off -for "${secs}s" -send tone -keys synth -sound $how "$@"
        probe $B "$name-$how-b" -peer "$IPA" -role b -page off -for "${secs}s" -send tone -keys synth -sound $how "$@"
        [[ -n ${DURING:-} ]] && incus exec $B -- sh -c "$DURING"
        wait
        fetch $A "$name-$how-a"
        fetch $B "$name-$how-b"
      done
    }
    echo "== the processor, a tone each way and keys, 90 s"
    ab rtp-cpu 90
    grep -H -o 'share=[0-9.]*%' $LOGS/rtp-cpu-*.log | paste -sd' ' | fold -w 160
    grep -H 'done after' $LOGS/rtp-cpu-*.log | grep -o 'rtp-cpu-[a-z-]*\|cpu=.*' | paste -sd' '
    echo "== short cuts: 200 ms six times, then 1 s three times, SCTP's timer as Pion has it"
    DURING='sh /root/blips.sh' ab rtp-blips 80 -rto 0
    for how in sctp rtp; do
      echo "$how: what a received each ten seconds"
      grep -h 'sound rx' "$LOGS/rtp-blips-$how-a.log" | grep -oE 'packets=[0-9]+ lost=[0-9]+|gap-max=[0-9.]+ms' | paste -sd' ' | fold -w 150
      grep -c 'quiet: nothing heard' "$LOGS/rtp-blips-$how-a.log" | sed 's/^/times the link went quiet: /'
    done
    for pct in 1 5; do
      echo "== $pct% of packets lost at random each way, SCTP's timer capped at 100 ms, 60 s"
      DURING="nft 'add table inet relayloss; add chain inet relayloss in { type filter hook input priority -10; policy accept; }; add chain inet relayloss out { type filter hook output priority -10; policy accept; }; add rule inet relayloss in udp dport $PORT numgen random mod 100 < $pct drop; add rule inet relayloss out udp sport $PORT numgen random mod 100 < $pct drop'; sleep 62; nft delete table inet relayloss" \
        ab "rtp-loss$pct" 60
      for how in sctp rtp; do
        echo "$how: what a received each ten seconds, then what b sent"
        grep -h 'sound rx' "$LOGS/rtp-loss$pct-$how-a.log" | grep -oE 'packets=[0-9]+ lost=[0-9]+|gap-max=[0-9.]+ms' | paste -sd' ' | fold -w 150
        grep -h 'sound tx' "$LOGS/rtp-loss$pct-$how-b.log" | grep -oE 'packets=[0-9]+ dropped=[0-9]+|queued-max=[0-9]+B' | paste -sd' ' | fold -w 150
      done
    done
    fw $A none
    fw $B none
    ;;
  *)
    sed -n '9,20p' "$0" >&2
    exit 2
    ;;
esac
