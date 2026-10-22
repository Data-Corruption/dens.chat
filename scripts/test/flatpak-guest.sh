#!/bin/sh

# Runs inside the Flatpak e2e's container as root, one step per call; see
# test-flatpak-e2e.sh for the flow. The member is alice, whose systemd user
# manager runs her session bus and Flatpak's portal, and every call to the
# Flatpak's local API goes through her paired browser session (a cookie
# jar).

set -eu

APP=chat.dens.Dens
JAR=/root/cookies
BASE=http://127.0.0.1:8494
REPO=/srv/dens/repo
PASSWORD='local password'
export GNUPGHOME=/root/gnupg

uid() { id -u alice; }

# as_alice runs a command as alice, in her session.
as_alice() {
    runuser -u alice -- env XDG_RUNTIME_DIR="/run/user/$(uid)" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$(uid)/bus" "$@"
}

dens() { as_alice flatpak run --command=dens "$APP" "$@"; }

data=/home/alice/.var/app/$APP/data/dens/main

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    for log in "$data"/data/logs/*.log; do
        [ -f "$log" ] && tail -n 40 "$log" >&2
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

# code METHOD PATH [JSON] prints the response's status alone.
code() {
    if [ $# -ge 3 ]; then
        curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $BASE" \
            -H "Content-Type: application/json" -d "$3" "$BASE$2"
    else
        curl -sS -o /dev/null -w '%{http_code}' -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $BASE" "$BASE$2"
    fi
}

# json EXPR evaluates a Python expression over the JSON on stdin, as d.
json() {
    python3 -c "import json, sys; d = json.load(sys.stdin); print($1)"
}

# sandboxes counts the sandboxes of Dens running.
sandboxes() {
    as_alice flatpak ps --columns=application | grep -cx "$APP" || :
}

wait_stopped() {
    for _ in $(seq 1 120); do
        [ "$(sandboxes)" = 0 ] && return 0
        sleep 0.25
    done
    fail "Dens didn't stop"
}

# wait_status EXPR waits until EXPR, over /api/status as d, is True.
wait_status() {
    for _ in $(seq 1 "${2:-240}"); do
        [ "$(api GET /api/status 2>/dev/null | json "$1" 2>/dev/null)" = True ] && return 0
        sleep 0.5
    done
    api GET /api/status >&2 || :
    fail "the status never had $1"
}

step=$1
shift
case $step in
image)
    # What the container image holds: Flatpak and what making its commits
    # takes, the member, her session, and the freedesktop runtime.
    # The portal's frontend holds the permission store, which says the app
    # may update itself; it has no backends here, so no keyring.
    bash /root/scripts/flatpak.sh deps
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
        xdg-desktop-portal dbus-user-session curl python3 ca-certificates >/dev/null
    useradd -m alice
    loginctl enable-linger alice
    for _ in $(seq 1 100); do
        [ -S "/run/user/$(uid)/bus" ] && break
        sleep 0.2
    done
    as_alice flatpak --user remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo
    as_alice flatpak --user install -y --noninteractive --no-related flathub org.freedesktop.Platform//26.08
    ;;
prepare)
    # An image made before the frontend was in it.
    dpkg -s xdg-desktop-portal >/dev/null 2>&1 ||
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends xdg-desktop-portal >/dev/null
    # Flatpak's portal checks for updates every few seconds here, where it
    # checks every 30 minutes.
    mkdir -p /etc/systemd/user/flatpak-portal.service.d
    printf '[Service]\nExecStart=\nExecStart=/usr/libexec/flatpak-portal --poll-timeout=3\n' \
        > /etc/systemd/user/flatpak-portal.service.d/poll.conf
    for _ in $(seq 1 100); do
        [ -S "/run/user/$(uid)/bus" ] && break
        sleep 0.2
    done
    as_alice systemctl --user daemon-reload
    # A signing key, as the release's repository has.
    mkdir -p "$GNUPGHOME"
    chmod 0700 "$GNUPGHOME"
    gpg --batch --passphrase '' --quick-gen-key 'Dens e2e <e2e@dens.chat>' ed25519 sign never 2>/dev/null
    mkdir -p /srv/dens
    gpg --batch --export --armor > /srv/dens/key.asc
    chmod -R a+rX /srv/dens
    ;;
commit)
    # commit VERSION: add the build of VERSION to the repository.
    GPG_KEY=$(gpg --list-keys --with-colons | awk -F: '/^fpr/ { print $10; exit }') \
        bash /root/scripts/flatpak.sh commit "/root/dens-$1" "$1" "$REPO"
    chmod -R a+rX /srv/dens
    gpgconf --kill gpg-agent 2>/dev/null || :
    ;;
install)
    as_alice flatpak --user remote-add --if-not-exists --gpg-import=/srv/dens/key.asc dens "file://$REPO"
    # A static binary needs none of the runtime's extensions.
    as_alice flatpak --user install -y --noninteractive --no-related dens "$APP"
    # The app may install its own updates, as KDE asks the member once.
    as_alice flatpak permission-set flatpak updates "$APP" yes
    ;;
launch-twice)
    # Two launches at once both open the page, from one service: each
    # starts one, and the one that loses the lifecycle lock to the other
    # gives up once it has waited for it.
    as_alice flatpak run "$APP" launch > /root/launch-1.out 2>&1 &
    first=$!
    as_alice flatpak run "$APP" launch > /root/launch-2.out 2>&1 &
    second=$!
    wait "$first" || fail "a launch failed: $(cat /root/launch-1.out)"
    wait "$second" || fail "a launch failed: $(cat /root/launch-2.out)"
    grep -c 'Opened Dens in your browser' /root/launch-1.out /root/launch-2.out | grep -c ':1$' | grep -qx 2 ||
        fail "the launches didn't both open the page: $(cat /root/launch-1.out /root/launch-2.out)"
    for _ in $(seq 1 180); do
        [ "$(sandboxes)" = 1 ] && exit 0
        sleep 0.25
    done
    fail "$(sandboxes) sandboxes of Dens run"
    ;;
link)
    # link: the page's pairing link, as dens open prints it.
    dens open --print
    ;;
launch)
    # launch: open Dens from the apps menu, as its desktop file does; print
    # the launch's output.
    as_alice flatpak run "$APP" launch 2>&1
    ;;
pair)
    # pair URL: pair with a pairing link, and set the local password where
    # there's none yet.
    api POST /api/pair "{\"token\":\"${1#*#token=}\"}" >/dev/null
    if [ "$(api GET /api/status | json 'd["passwordSet"]')" = False ]; then
        api POST /api/password "{\"password\":\"$PASSWORD\"}" >/dev/null
    fi
    ;;
status)
    api GET /api/status
    ;;
quit)
    api POST /api/quit >/dev/null
    wait_stopped
    ;;
unlock)
    [ "$(code POST /api/settings)" = 423 ] || fail "the settings answered while locked"
    [ "$(code POST /api/unlock '{"password":"wrong wrong wrong"}')" = 403 ] || fail "a wrong password unlocked Dens"
    api POST /api/unlock "{\"password\":\"$PASSWORD\"}" >/dev/null
    wait_status 'not d["locked"]' 20
    ;;
remember)
    # With no keyring to take it, the key is kept in a file. The portal
    # has no Secret interface, which Dens waits up to a minute for.
    api POST /api/remember "{\"on\":true,\"password\":\"$PASSWORD\"}" >/dev/null
    for _ in $(seq 1 180); do
        where=$(api GET /api/settings | json 'd["remember"].get("where", "") if not d["remember"].get("asking") else ""')
        [ "$where" = file ] && break
        sleep 0.5
    done
    [ "$where" = file ] || fail "Remember kept the key in '$where'"
    [ -f "$data/control/datakey.file" ] || fail "the key's file isn't there"
    ;;
trust)
    # trust FILE: have the Flatpak trust the den's certificate authority,
    # whose runtime has a store of its own.
    cp "$1" "/home/alice/.var/app/$APP/den-ca.pem"
    chown alice: "/home/alice/.var/app/$APP/den-ca.pem"
    as_alice flatpak override --user --env=SSL_CERT_FILE="/home/alice/.var/app/$APP/den-ca.pem" "$APP"
    ;;
hosts)
    printf '%s %s\n' "$1" "$2" >> /etc/hosts
    ;;
join)
    # join INVITE: join as bob, through the den's Caddy.
    api POST /api/dens/join "{\"invite\":\"$1\",\"username\":\"bob\",\"display_name\":\"Bob\",\"password\":\"bob password\"}" |
        json 'd["den"]["role"]'
    ;;
wait-connected)
    for _ in $(seq 1 120); do
        [ "$(api GET /api/dens | json 'd["dens"][0]["state"] if d["dens"] else ""')" = connected ] && exit 0
        sleep 0.5
    done
    api GET /api/dens >&2
    fail "the den didn't connect"
    ;;
wait-update)
    # Flatpak's update monitor finds the build the repository has now.
    wait_status '"flatpakUpdate" in d and d["flatpakUpdate"]["available"] and d["flatpakUpdate"]["can_install"]'
    ;;
update)
    # update VERSION: install the update from the page, which restarts
    # into it; it migrates after a snapshot of the data before.
    api POST /api/update >/dev/null
    wait_status "d[\"version\"] == \"$1\""
    [ "$(sandboxes)" = 1 ] || fail "$(sandboxes) sandboxes of Dens run after the update"
    python3 -c "import json; d = json.load(open('$data/control/state.json')); print(d['phase'], d['version'])" |
        grep -qx "ready $1" || fail "the state after the update: $(cat "$data/control/state.json")"
    ls "$data"/snapshot/*.backup >/dev/null 2>&1 || fail "the update kept no snapshot"
    ;;
rollback)
    # rollback VERSION: roll Dens back to the build of VERSION, as a member
    # can with flatpak, and launch it, which refuses the newer data.
    commit=$(ostree log --repo="$REPO" "app/$APP/$(uname -m)/stable" | awk -v v="Dens $1" '
        /^commit / { c = $2 } $0 ~ v { print c; exit }')
    [ -n "$commit" ] || fail "no commit of $1"
    as_alice flatpak update --user -y --noninteractive --commit="$commit" "$APP" >/dev/null
    api POST /api/quit >/dev/null || :
    wait_stopped
    if out=$(as_alice flatpak run "$APP" launch 2>&1); then
        fail "a rolled-back build started: $out"
    fi
    printf '%s\n' "$out" | grep -q 'rolled back' || fail "the refusal: $out"
    printf '%s\n' "$out" | grep -q 'restore' || fail "the refusal named no snapshot: $out"
    ;;
restore)
    # Restore the snapshot of the data from before the update, which the
    # rolled-back build reads; it starts locked, its remembered key gone.
    snapshot=$(ls "$data"/snapshot/*.backup)
    printf '%s\n' "$PASSWORD" | dens restore --yes --password-stdin "$snapshot"
    [ ! -e "$data/control/datakey.file" ] || fail "the restore kept the remembered key"
    ;;
*)
    fail "unknown step $step"
    ;;
esac
