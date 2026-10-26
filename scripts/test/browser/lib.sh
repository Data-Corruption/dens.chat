#!/usr/bin/env bash
# Sourced by each check's prep script in the browser container, as tester:
# development instances on loopback, started and paired from scratch, and
# what the checks need of the den, made through each member's API.
#
#   m2a  alice, who hosts the den: page 18484, den 18485, media 17881/17882
#   m2b  bob, a member: page 18486
#   m2c  carol, a member when a check needs one: page 18487
#
# The instances run in the background with their PIDs in ~/<instance>.pid
# and their output in ~/<instance>.log.

set -euo pipefail
cd "$HOME"
export XDG_RUNTIME_DIR=/run/user/$UID

declare -A PORT=([alice]=18484 [bob]=18486 [carol]=18487)
declare -A INSTANCE=([alice]=m2a [bob]=m2b [carol]=m2c)
DEN_URL=${DEN_URL:-http://127.0.0.1:18485}

# api WHO METHOD PATH [JSON] calls WHO's page API as their paired browser.
api() {
    local jar="$HOME/jar-${INSTANCE[$1]}" base="http://127.0.0.1:${PORT[$1]}"
    if [[ $# -ge 4 ]]; then
        curl -sS --fail-with-body -b "$jar" -c "$jar" -X "$2" -H "Origin: $base" -H "Content-Type: application/json" --data "$4" "$base$3"
    else
        curl -sS --fail-with-body -b "$jar" -c "$jar" -X "$2" -H "Origin: $base" "$base$3"
    fi
}

# json EXPR prints EXPR of the JSON on standard input, as d.
json() { python3 -c "import json, sys; d = json.load(sys.stdin); print($1)"; }

nonce() { python3 -c 'import base64, os; print(base64.urlsafe_b64encode(os.urandom(16)).decode().rstrip("="))'; }

stop() {
    local pid
    [[ -f $1.pid ]] || return 0
    pid=$(cat "$1.pid")
    kill "$pid" 2>/dev/null || true
    while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
    rm -f "$1.pid"
}

# start INSTANCE [FLAGS...] starts an instance and waits for its page.
start() {
    local name=$1 port
    shift
    port=$(sed -n 's/.*--port \([0-9]*\).*/\1/p' <<<"$*")
    nohup ./dens-dev -i "$name" service run "$@" >"$name.log" 2>&1 &
    echo $! >"$name.pid"
    for _ in $(seq 100); do
        curl -s -o /dev/null "http://127.0.0.1:$port/api/status" && return 0
        sleep 0.1
    done
    echo "instance $name didn't start: $(cat "$name.log")" >&2
    return 1
}

start_den() { start m2a --port 18484 --den-port 18485 --media-udp-port 17881 --media-tcp-port 17882; }

# pair WHO pairs WHO's instance with a cookie jar, as a browser, and sets
# the local password.
pair() {
    local url
    url=$(./dens-dev -i "${INSTANCE[$1]}" open --print)
    api "$1" POST /api/pair "{\"token\":\"${url#*#token=}\"}" >/dev/null
    api "$1" POST /api/password '{"password":"local password"}' >/dev/null
}

# invite prints a one-use invite to the den, from alice.
invite() { api alice POST "/api/dens/$DEN/invites" '{"expires_in":3600,"max_uses":1}' | json 'd["invite"]'; }

# join WHO starts WHO's instance, if it isn't alice's or bob's, and joins
# them to the den.
join() {
    local name=${INSTANCE[$1]}
    [[ $1 == bob ]] || start "$name" --port "${PORT[$1]}"
    pair "$1"
    api "$1" POST /api/dens/join "{\"invite\":\"$(invite)\",\"username\":\"$1\",\"display_name\":\"${1^}\",\"password\":\"$1 password\"}" >/dev/null
}

# fresh starts over: the display and sound the browsers use, then new
# instances for alice and bob, with a den alice owns at DEN_URL, its voice
# channel Lounge, and bob in it. It sets DEN and LOUNGE.
fresh() {
    local name
    for name in m2a m2b m2c; do stop "$name"; done
    rm -rf "$HOME/.local/share/dens-dev"/m2? "$HOME"/jar-m2?
    pgrep -u "$(id -u)" -x Xvfb >/dev/null || { nohup Xvfb :99 -screen 0 1600x900x24 >xvfb.log 2>&1 & }
    pulseaudio --check 2>/dev/null || pulseaudio --start --exit-idle-time=-1
    start_den
    start m2b --port 18486
    pair alice
    api alice POST /api/den "{\"name\":\"Voice Den\",\"url\":\"$DEN_URL\",\"username\":\"alice\",\"display_name\":\"Alice\",\"password\":\"den password\"}" >/dev/null
    DEN=$(api alice GET /api/dens | json 'next(x["den_id"] for x in d["dens"] if x["own"])')
    join bob
    # shellcheck disable=SC2034 # the prep scripts print it
    LOUNGE=$(channel Lounge voice)
}

# channel NAME [KIND] [GROUP] makes a channel as alice, a text one unless
# KIND is voice, in the group whose ID is GROUP if there is one, and prints
# its ID once bob has it.
channel() {
    local id group=""
    [[ -n ${3:-} ]] && group=",\"group_id\":\"$3\""
    api alice POST "/api/dens/$DEN/channels" "{\"name\":\"$1\",\"kind\":\"${2:-text}\"$group}" >/dev/null
    for _ in $(seq 80); do
        id=$(api bob GET "/api/dens/$DEN/state" | json "next((c['id'] for c in d['channels'] if c['name'] == '$1'), '')")
        [[ -n $id ]] && { echo "$id"; return 0; }
        sleep 0.25
    done
    echo "bob never saw channel $1" >&2
    return 1
}

# group NAME makes a channel group as alice, and prints its ID once bob
# has it.
group() {
    local id
    api alice POST "/api/dens/$DEN/groups" "{\"name\":\"$1\"}" >/dev/null
    for _ in $(seq 80); do
        id=$(api bob GET "/api/dens/$DEN/state" | json "next((g['id'] for g in d['groups'] if g['name'] == '$1'), '')")
        [[ -n $id ]] && { echo "$id"; return 0; }
        sleep 0.25
    done
    echo "bob never saw group $1" >&2
    return 1
}

# upload WHO FILE NAME uploads a file as WHO and prints its ID.
upload() {
    curl -sS --fail-with-body -b "$HOME/jar-${INSTANCE[$1]}" -c "$HOME/jar-${INSTANCE[$1]}" -X POST \
        -H "Origin: http://127.0.0.1:${PORT[$1]}" -H "Content-Type: application/octet-stream" -H "Dens-Filename: $3" \
        --data-binary "@$2" "http://127.0.0.1:${PORT[$1]}/api/dens/$DEN/uploads" | json 'd["id"]'
}

# send WHO CHANNEL TEXT [FILE...] sends a message as WHO, with uploads by
# ID, and prints its ID. TEXT goes into JSON as it is.
send() {
    local who=$1 channel=$2 text=$3 files=""
    shift 3
    if (($#)); then
        files=$(printf '"%s",' "$@")
        files=${files%,}
    fi
    api "$who" POST "/api/dens/$DEN/channels/$channel/messages" \
        "{\"nonce\":\"$(nonce)\",\"text\":\"$text\",\"attachments\":[$files]}" | json 'd["id"]'
}

# restart_den restarts alice's instance in place, as an update would.
restart_den() {
    stop m2a
    start_den
}

# speak plays speech.wav into PulseAudio's null sink "speech", over and
# over, and makes its monitor the default source, which Firefox takes for
# its microphone with media 'pulse': Firefox's own fake one is a tone,
# which RNNoise takes for noise. The browsers play into another null sink,
# "speakers".
speak() {
    local sink
    for sink in speech speakers; do
        pactl list short sinks | cut -f2 | grep -qx "$sink" || pactl load-module module-null-sink "sink_name=$sink" >/dev/null
    done
    pactl set-default-sink speakers
    pactl set-default-source speech.monitor
    if [[ -f speech.pid ]] && kill -0 "$(cat speech.pid)" 2>/dev/null; then
        return 0
    fi
    nohup bash -c 'while :; do paplay -d speech speech.wav; done' >/dev/null 2>&1 &
    echo $! >speech.pid
}
