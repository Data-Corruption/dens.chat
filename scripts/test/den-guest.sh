#!/bin/sh

# Runs inside a den e2e container as root, one step per call; see
# test-den-e2e.sh for the flow. The desktop user is alice, and every call to
# the local API goes through her paired browser session (a cookie jar).

set -eu

JAR=/root/cookies
BASE=http://127.0.0.1:8484

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    for unit in dens@main caddy-e2e; do
        systemctl status "$unit" --no-pager 2>/dev/null | head -n 15 >&2 || :
        journalctl -u "$unit" -b --no-pager -o cat 2>/dev/null | tail -n 30 >&2 || :
    done
    exit 1
}

# api METHOD PATH [JSON] prints the response body and fails on an error status.
api() {
    if [ $# -ge 3 ]; then
        curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $BASE" \
            -H "Content-Type: application/json" -d "$3" "$BASE$2"
    else
        curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $BASE" "$BASE$2"
    fi
}

# json EXPR evaluates a Python expression over the JSON on stdin, as d.
json() {
    python3 -c "import json, sys; d = json.load(sys.stdin); print($1)"
}

step=$1
shift
case $step in
prepare)
    # LXC's drop-in turns off NoNewPrivileges, credentials and more for
    # every service; mask it so the unit runs as it would on a real machine.
    if [ -e /run/systemd/system/service.d/zzz-lxc-service.conf ]; then
        mkdir -p /etc/systemd/system/service.d
        ln -sf /dev/null /etc/systemd/system/service.d/zzz-lxc-service.conf
        systemctl daemon-reload
    fi
    missing=""
    for tool in curl gzip python3; do
        command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
    done
    # shellcheck disable=SC2086 # $missing is a list of package names
    if [ -n "$missing" ]; then
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq && apt-get install -y -qq --no-install-recommends ca-certificates $missing
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y -q $missing
        elif command -v pacman >/dev/null 2>&1; then
            pacman -Sy --noconfirm --needed --quiet $missing
        fi
    fi
    useradd -m alice
    ;;
install)
    SUDO_UID=$(id -u alice) SUDO_USER=alice APP_SKIP_VERIFY=true APP_RELEASE_URL=file:///release/ \
        sh /release/install.sh "$@"
    ;;
pair)
    url=$(runuser -u alice -- dens open --print)
    api POST /api/pair "{\"token\":\"${url#*#token=}\"}" >/dev/null
    api POST /api/password '{"password":"local password"}' >/dev/null
    ;;
caddy)
    # caddy DEN_PORT: serve den.test with Caddy's internal CA, as root.
    mkdir -p /etc/caddy
    cat > /etc/caddy/Caddyfile <<EOF
{
	skip_install_trust
}
den.test {
	tls internal
	reverse_proxy 127.0.0.1:$1
}
EOF
    systemd-run --unit=caddy-e2e --property=Restart=on-failure --setenv=HOME=/root \
        /usr/local/bin/caddy run --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
    root=/root/.local/share/caddy/pki/authorities/local/root.crt
    for _ in $(seq 1 100); do
        [ -f "$root" ] && exit 0
        sleep 0.2
    done
    fail "Caddy didn't create its certificate authority"
    ;;
trust)
    # trust FILE: add a CA certificate to the system store.
    if command -v update-ca-certificates >/dev/null 2>&1 && [ -d /usr/local/share/ca-certificates ]; then
        cp "$1" /usr/local/share/ca-certificates/dens-e2e.crt
        update-ca-certificates >/dev/null
    elif [ -d /etc/pki/ca-trust/source/anchors ]; then
        cp "$1" /etc/pki/ca-trust/source/anchors/dens-e2e.crt
        update-ca-trust
    elif [ -d /etc/ca-certificates/trust-source/anchors ]; then
        cp "$1" /etc/ca-certificates/trust-source/anchors/dens-e2e.crt
        update-ca-trust
    else
        fail "no system certificate store found"
    fi
    ;;
hosts)
    printf '%s %s\n' "$1" "$2" >> /etc/hosts
    ;;
create-den)
    # create-den URL: create the den and its owner; print its ID.
    api POST /api/den "{\"name\":\"E2E Den\",\"url\":\"$1\",\"username\":\"alice\",\"display_name\":\"Alice\",\"password\":\"den password\"}" |
        json 'd["den"]["den_id"] if len(d["recovery_codes"]) == 10 else ""'
    ;;
invite)
    api POST "/api/dens/$1/invites" '{"expires_in":3600,"max_uses":1}' | json 'd["invite"]'
    ;;
join)
    name=$(api POST /api/dens/preview "{\"invite\":\"$1\"}" | json 'd["den"]["name"]')
    [ "$name" = "E2E Den" ] || fail "the preview named the den $name"
    api POST /api/dens/join "{\"invite\":\"$1\",\"username\":\"bob\",\"display_name\":\"Bob\",\"password\":\"bob password\"}" |
        json 'd["den"]["role"]'
    ;;
wait-connected)
    # wait-connected SINCE: wait until the only den is connected, with a
    # state newer than SINCE (ms); print when it connected.
    for _ in $(seq 1 120); do
        since=$(api GET /api/dens | json 'd["dens"][0]["since"] if d["dens"] and d["dens"][0]["state"] == "connected" else 0')
        if [ "$since" -gt "$1" ]; then
            printf '%s\n' "$since"
            exit 0
        fi
        sleep 0.5
    done
    api GET /api/dens >&2
    fail "the den didn't connect"
    ;;
status)
    api GET /api/dens
    ;;
*)
    printf 'unknown step %s\n' "$step" >&2
    exit 2
    ;;
esac
