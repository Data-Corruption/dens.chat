#!/bin/sh

# Runs inside a den e2e container as root, one step per call; see
# test-den-e2e.sh for the flow. The desktop user is alice, and every call to
# the local API goes through her paired browser session (a cookie jar).
# INSTANCE and PORT pick another instance on the machine than main on 8484.

set -eu

INSTANCE=${INSTANCE:-main}
JAR=/root/cookies-$INSTANCE
BASE=http://127.0.0.1:${PORT:-8484}

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    for unit in "dens@$INSTANCE" caddy-e2e; do
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
    url=$(runuser -u alice -- dens open --print --instance "$INSTANCE")
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
    # join INVITE: join as bob; print the role, and keep the recovery codes.
    name=$(api POST /api/dens/preview "{\"invite\":\"$1\"}" | json 'd["den"]["name"]')
    [ "$name" = "E2E Den" ] || fail "the preview named the den $name"
    api POST /api/dens/join "{\"invite\":\"$1\",\"username\":\"bob\",\"display_name\":\"Bob\",\"password\":\"bob password\"}" \
        > /root/join.json
    json 'd["den"]["role"] if len(d["recovery_codes"]) == 10 else "no codes"' < /root/join.json
    ;;
code)
    # code N: print the Nth recovery code from joining.
    json "d['recovery_codes'][$1]" < /root/join.json
    ;;
fingerprint)
    # fingerprint: print the only den's ID as the page shows it.
    api GET /api/dens | json 'd["dens"][0]["fingerprint"]'
    ;;
recover)
    # recover DEN USERNAME CODE PASSWORD: sign this instance in to a den it's
    # new to with a recovery code, which sets a new password; print how many
    # codes are left, how many other devices it signed out, and the den's ID
    # as the page shows it.
    api POST /api/dens/signin "{\"den\":\"$1\",\"username\":\"$2\",\"recovery_code\":\"$3\",\"password\":\"$4\"}" |
        json '"%d %d %s" % (d["recovery_codes_left"], d["signed_out"], d["den"]["fingerprint"])'
    ;;
sign-in)
    # sign-in DEN USERNAME PASSWORD: sign this instance in with the den
    # password.
    api POST /api/dens/signin "{\"den\":\"$1\",\"username\":\"$2\",\"password\":\"$3\"}" >/dev/null
    ;;
sign-in-refused)
    # sign-in-refused DEN USERNAME PASSWORD: print why signing in failed.
    status=$(curl -sS -b "$JAR" -c "$JAR" -X POST -H "Origin: $BASE" -H "Content-Type: application/json" \
        -d "{\"den\":\"$1\",\"username\":\"$2\",\"password\":\"$3\"}" \
        -o /root/signin.json -w '%{http_code}' "$BASE/api/dens/signin")
    [ "$status" -ge 400 ] || fail "signing in was accepted"
    json 'd["error"]' < /root/signin.json
    ;;
other-device)
    # other-device DEN_ID: print the key ID of this member's other device,
    # failing unless they have exactly two.
    key=$(api GET "/api/dens/$1/devices" |
        json '[x["key_id"] for x in d["devices"] if not x.get("current")][0] if len(d["devices"]) == 2 else ""')
    [ -n "$key" ] || fail "the member doesn't have exactly two devices"
    printf '%s\n' "$key"
    ;;
revoke)
    # revoke DEN_ID KEY_ID: sign one of this member's devices out.
    api DELETE "/api/dens/$1/devices/$2" >/dev/null
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
channel)
    # channel DEN_ID NAME: create a text channel; print its ID once the
    # den's event has reached this client.
    api POST "/api/dens/$1/channels" "{\"name\":\"$2\"}" >/dev/null
    for _ in $(seq 1 40); do
        id=$(api GET "/api/dens/$1/state" | json 'next((c["id"] for c in d["channels"] if c["name"] == "'"$2"'"), "")')
        [ -n "$id" ] && { printf '%s\n' "$id"; exit 0; }
        sleep 0.25
    done
    fail "channel $2 never appeared"
    ;;
wait-channel)
    # wait-channel DEN_ID: print the first channel's ID once one exists.
    for _ in $(seq 1 40); do
        id=$(api GET "/api/dens/$1/state" | json 'd["channels"][0]["id"] if d["channels"] else ""')
        [ -n "$id" ] && { printf '%s\n' "$id"; exit 0; }
        sleep 0.25
    done
    fail "no channel reached this client"
    ;;
send)
    # send DEN_ID CHANNEL_ID TEXT: post a message; print its ID.
    nonce=$(python3 -c 'import base64, os; print(base64.urlsafe_b64encode(os.urandom(16)).decode().rstrip("="))')
    api POST "/api/dens/$1/channels/$2/messages" "{\"nonce\":\"$nonce\",\"text\":\"$3\"}" | json 'd["id"]'
    ;;
history)
    # history DEN_ID CHANNEL_ID: print the channel's messages, oldest first.
    api GET "/api/dens/$1/channels/$2/messages?limit=100" | json '"\n".join(m["text"] for m in d["messages"])'
    ;;
member-id)
    # member-id DEN_ID USERNAME: print a member's ID.
    api GET "/api/dens/$1/state" | json 'next(m["id"] for m in d["members"] if m["username"] == "'"$2"'")'
    ;;
dm)
    # dm DEN_ID MEMBER_ID: open the DM with a member; print its ID.
    api POST "/api/dens/$1/dms" "{\"member_id\":\"$2\"}" | json 'd["id"]'
    ;;
wait-dm)
    # wait-dm DEN_ID: print this member's DM's ID once it has arrived.
    for _ in $(seq 1 40); do
        id=$(api GET "/api/dens/$1/state" | json 'next((c["id"] for c in d["channels"] if c["kind"] == "dm"), "")')
        [ -n "$id" ] && { printf '%s\n' "$id"; exit 0; }
        sleep 0.25
    done
    fail "no DM reached this client"
    ;;
ban)
    # ban DEN_ID MEMBER_ID: remove a member and keep them out.
    api POST "/api/dens/$1/members/$2/remove" '{"ban":true}' >/dev/null
    ;;
wait-out)
    # wait-out STATE: wait until the only den has closed this device out,
    # as revoked or removed; print the reason it gives.
    for _ in $(seq 1 100); do
        reason=$(api GET /api/dens | json 'd["dens"][0]["error"] if d["dens"][0]["state"] == "'"$1"'" else ""')
        [ -n "$reason" ] && { printf '%s\n' "$reason"; exit 0; }
        sleep 0.1
    done
    api GET /api/dens >&2
    fail "the den never closed this device out as $1"
    ;;
upload)
    # upload DEN_ID FILE NAME: upload a file as the page does; print its ID,
    # whether metadata came out, its size and whether it has a preview.
    curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X POST -H "Origin: $BASE" \
        -H "Content-Type: application/octet-stream" -H "Dens-Filename: $3" \
        --data-binary "@$2" "$BASE/api/dens/$1/uploads" |
        json 'd["id"] + " " + str(d.get("stripped", False)).lower() + " %dx%d " % (d["width"], d["height"]) + ("preview" if d.get("thumb") else "none")'
    ;;
send-file)
    # send-file DEN_ID CHANNEL_ID FILE_ID: send an upload with no text; print
    # the message's ID.
    nonce=$(python3 -c 'import base64, os; print(base64.urlsafe_b64encode(os.urandom(16)).decode().rstrip("="))')
    api POST "/api/dens/$1/channels/$2/messages" "{\"nonce\":\"$nonce\",\"text\":\"\",\"attachments\":[\"$3\"]}" | json 'd["id"]'
    ;;
fetch)
    # fetch DEN_ID FILE_ID OUT [thumb]: save a file, or its preview, as the
    # page gets it; print the type it came as.
    path="/api/dens/$1/files/$2"
    [ "${4:-}" = thumb ] && path="$path/thumb"
    curl -sS --fail-with-body -b "$JAR" -o "$3" -D /root/headers "$BASE$path"
    sed -n 's/^[Cc]ontent-[Tt]ype: *\([^;\r]*\).*/\1/p' /root/headers
    ;;
jpeg)
    # jpeg FILE: print a JPEG's stored size and what it carries besides the
    # image, failing if anything of the phone photo's metadata is left.
    python3 - "$1" <<'PY'
import struct, sys
d = open(sys.argv[1], "rb").read()
for marker in (b"TestPhone", b"GPSLatitude", b"Taken at home", b"ns.adobe.com"):
    if marker in d:
        sys.exit("the file still carries " + marker.decode())
i, kept, size = 2, [], None
while i + 4 <= len(d) and d[i] == 0xFF and d[i + 1] != 0xDA:
    m = d[i + 1]
    n = struct.unpack(">H", d[i + 2:i + 4])[0]
    p = d[i + 4:i + 2 + n]
    if p.startswith(b"Exif\0\0"):
        kept.append("orientation %d" % p[25])
    elif 0xE0 <= m <= 0xEF or m == 0xFE:
        kept.append(p.split(b"\0")[0].decode("latin-1"))
    elif 0xC0 <= m <= 0xC3:
        size = (struct.unpack(">H", p[3:5])[0], struct.unpack(">H", p[1:3])[0])
    i += 2 + n
print("%dx%d %s" % (size[0], size[1], ",".join(kept) or "nothing"))
PY
    ;;
sealed)
    # sealed: fail if any stored upload holds a JPEG in the clear.
    for f in /var/lib/dens/main/data/uploads/*; do
        if grep -q JFIF "$f"; then
            fail "$f is stored in the clear"
        fi
    done
    find /var/lib/dens/main/data/uploads -type f | wc -l
    ;;
join-refused)
    # join-refused INVITE: try to join as bob again; print the refusal.
    status=$(curl -sS -b "$JAR" -c "$JAR" -X POST -H "Origin: $BASE" -H "Content-Type: application/json" \
        -d "{\"invite\":\"$1\",\"username\":\"bob\",\"display_name\":\"Bob\",\"password\":\"bob password\"}" \
        -o /root/join.json -w '%{http_code}' "$BASE/api/dens/join")
    [ "$status" -ge 400 ] || fail "joining again was accepted"
    json 'd["error"]' < /root/join.json
    ;;
*)
    printf 'unknown step %s\n' "$step" >&2
    exit 2
    ;;
esac
