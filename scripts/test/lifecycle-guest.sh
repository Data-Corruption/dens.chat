#!/bin/sh

# Runs inside an e2e container as root. test-lifecycle-e2e.sh calls it
# twice, with a reboot in between:
#   lifecycle-guest.sh install        prepare, host a den with the first
#                                     release and update it, then install,
#                                     pair, turn hosting on and off with
#                                     dens den hosting, back up
#   lifecycle-guest.sh after-reboot   check, update, second instance,
#                                     restore, uninstall
# or once, for a release built from another commit, whose behavior the
# phases above don't describe:
#   lifecycle-guest.sh installer      prepare, install through the
#                                     installer, verifying the release,
#                                     check it answers, uninstall
# Environment: V1 and V2 are the fixture versions; V2 is empty when the
# release has no newer version to update to. /release/first holds Dens's
# first release.

set -eu

PASSWORD="correct horse battery staple"
CLIENT=http://127.0.0.1:8484
SECOND=http://127.0.0.1:18484
JAR=/root/cookies

step() { printf '\n== %s\n' "$*"; }
fail() {
    printf 'FAIL: %s\n' "$*" >&2
    for unit in dens@main dens@second; do
        systemctl status "$unit" --no-pager 2>/dev/null | head -n 15 >&2 || :
        journalctl -u "$unit" -b --no-pager -o cat 2>/dev/null | tail -n 30 >&2 || :
    done
    exit 1
}
as_alice() { runuser -u alice -- "$@"; }
alice_uid() { id -u alice; }

# api_at BASE JAR METHOD PATH [JSON] calls an instance's page at BASE with
# the browser session in JAR, prints the response body, and fails on an
# error status.
api_at() {
    base=$1 jar=$2
    shift 2
    if [ $# -ge 3 ]; then
        curl -sS --fail-with-body -b "$jar" -c "$jar" -X "$1" -H "Origin: $base" \
            -H "Content-Type: application/json" -d "$3" "$base$2"
    else
        curl -sS --fail-with-body -b "$jar" -c "$jar" -X "$1" -H "Origin: $base" "$base$2"
    fi
}

# api METHOD PATH [JSON] calls the main instance's page; second_api, the
# second instance's.
api() { api_at "$CLIENT" "$JAR" "$@"; }
second_api() { api_at "$SECOND" /root/cookies-second "$@"; }

expect_state() {
    state=$(cat "/var/lib/dens/$1/control/state.json")
    printf '%s\n' "$state" | grep -q "\"phase\":\"ready\",\"version\":\"$2\"" ||
        fail "instance $1 state is $state, want ready $2"
}

expect_mode() {
    actual=$(stat -c '%U:%G %a' "$1")
    [ "$actual" = "$2" ] || fail "$1 is $actual, want $2"
}

# den_answers PORT succeeds while a den listener answers on PORT.
den_answers() {
    curl -sS --fail -o /dev/null "http://127.0.0.1:$1/healthz" 2>/dev/null
}

# pair_alice pairs the browser session in $JAR and sets the local password.
pair_alice() {
    url=$(as_alice dens open --print)
    api POST /api/pair "{\"token\":\"${url#*#token=}\"}" >/dev/null
    api POST /api/password "{\"password\":\"$PASSWORD\"}" >/dev/null
}

# wait_own_connected waits for the page's view to show the den this
# instance hosts as connected.
wait_own_connected() {
    for _ in $(seq 1 60); do
        api GET /api/dens | grep -q '"own":true,"state":"connected"' && return 0
        sleep 1
    done
    fail "the den this instance hosts isn't connected: $(api GET /api/dens)"
}

expect_uninstalled() {
    for path in /var/lib/dens /usr/local/bin/dens /etc/systemd/system/dens@.service /etc/sysusers.d/dens-main.conf \
        /etc/xdg/autostart/chat.dens.Dens.desktop /usr/local/share/applications/chat.dens.Dens.desktop \
        /usr/local/share/icons/hicolor/scalable/apps/chat.dens.Dens.svg; do
        [ ! -e "$path" ] || fail "$path survived uninstall"
    done
    if id dens-main >/dev/null 2>&1; then
        fail "the dens-main account survived uninstall"
    fi
}

# first_release hosts a den with Dens's first release, as its installer's
# den option did, and updates to this one, which still hosts it: the switch
# takes the den role it had (M9.1). Then it uninstalls, for the fresh
# install after it.
first_release() {
    first=$(cat /release/first/version)
    case $(uname -m) in
        x86_64) first_bin=/release/first/linux-amd64 ;;
        aarch64) first_bin=/release/first/linux-arm64 ;;
        *) fail "no first release binary for $(uname -m)" ;;
    esac
    step "host a den with $first"
    # As root, as its installer ran it once it had verified the release.
    "$first_bin" install --den --user alice --release-url file:///release/
    systemctl is-active --quiet dens@main || fail "$first's dens@main is not running"
    expect_state main "$first"
    pair_alice
    api POST /api/den '{"name":"First Den","url":"https://den.test","username":"alice","display_name":"Alice","password":"den password"}' >/dev/null
    wait_own_connected
    den_answers 8485 || fail "$first's den listener doesn't answer"

    step "update the den $first hosts to $V1"
    SUDO_UID=$(alice_uid) SUDO_USER=alice sh /release/install.sh --release-url file:///release/ --skip-verify
    expect_state main "$V1"
    as_alice dens status | grep -q "version:   $V1" || fail "the service didn't come back as $V1"
    as_alice dens status | grep -q "den:       hosting a den" || fail "the den $first hosted isn't hosted after the update"
    den_answers 8485 || fail "the den listener doesn't answer after the update"
    api GET /api/hosting | grep -q '"enabled":true,"serving":true' || fail "hosting after the update: $(api GET /api/hosting)"
    api GET /api/dens | grep -q '"name":"First Den"' || fail "the den is gone after the update: $(api GET /api/dens)"
    wait_own_connected
    dens uninstall --yes
    expect_uninstalled
    rm -f "$JAR"
}

# prepare readies the container: the unit's real sandbox, the tools the
# installer and the harness need, and the users.
prepare() {
    step "prepare the container"
    # LXC's drop-in turns off NoNewPrivileges, credentials and more for every
    # service; mask it so the unit runs as it would on a real machine.
    if [ -e /run/systemd/system/service.d/zzz-lxc-service.conf ]; then
        mkdir -p /etc/systemd/system/service.d
        ln -sf /dev/null /etc/systemd/system/service.d/zzz-lxc-service.conf
        systemctl daemon-reload
    fi
    # Minimal images can lack the installer's tools or the harness's.
    # Each entry is tool:apt-package:dnf-package:pacman-package.
    missing=""
    for entry in curl:curl:curl:curl gzip:gzip:gzip:gzip runuser:util-linux:util-linux:util-linux \
        useradd:passwd:shadow-utils:shadow; do
        command -v "${entry%%:*}" >/dev/null 2>&1 && continue
        if command -v apt-get >/dev/null 2>&1; then
            missing="$missing $(echo "$entry" | cut -d: -f2)"
        elif command -v dnf >/dev/null 2>&1; then
            missing="$missing $(echo "$entry" | cut -d: -f3)"
        else
            missing="$missing $(echo "$entry" | cut -d: -f4)"
        fi
    done
    if [ -n "$missing" ]; then
        printf 'installing%s\n' "$missing"
        # shellcheck disable=SC2086 # a list of package names
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends $missing
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y -q $missing
        else
            pacman -Sy --noconfirm --needed --quiet $missing
        fi
    fi
    useradd -m alice
    useradd -m mallory
}

phase_install() {
    prepare
    [ ! -d /release/first ] || first_release

    step "the installer refuses v1.0's den option"
    if SUDO_UID=$(alice_uid) SUDO_USER=alice sh /release/install.sh --release-url file:///release/ --skip-verify --den \
        >/tmp/den-option.out 2>&1; then
        fail "the installer took --den"
    fi
    grep -q "hosting is turned on and off in Dens's settings" /tmp/den-option.out ||
        fail "unexpected refusal of --den: $(cat /tmp/den-option.out)"
    [ ! -e /var/lib/dens/main ] || fail "the refused install changed the system"

    step "install as alice through sudo"
    SUDO_UID=$(alice_uid) SUDO_USER=alice sh /release/install.sh --release-url file:///release/ --skip-verify
    systemctl is-active --quiet dens@main || fail "dens@main is not running"
    expect_state main "$V1"
    # The shortcut helper starts at each desktop sign-in, and the .desktop
    # file its portal needs names Dens, with its icon.
    grep -qx 'Exec=/usr/local/bin/dens shortcuts' /etc/xdg/autostart/chat.dens.Dens.desktop ||
        fail "no autostart entry starts the shortcut helper"
    grep -qx 'Exec=/usr/local/bin/dens open' /usr/local/share/applications/chat.dens.Dens.desktop ||
        fail "no .desktop file names Dens"
    grep -q '<svg' /usr/local/share/icons/hicolor/scalable/apps/chat.dens.Dens.svg || fail "Dens's icon is missing"
    expect_mode /etc/xdg/autostart/chat.dens.Dens.desktop "root:root 644"
    expect_mode /var/lib/dens/main/control "root:dens-main 750"
    expect_mode /var/lib/dens/main/control/state.json "root:dens-main 640"
    expect_mode /var/lib/dens/main/control/datakey.cred "root:root 600"
    expect_mode /var/lib/dens/main/data "dens-main:dens-main 700"
    pid=$(systemctl show -p MainPID --value dens@main)
    grep -q '^NoNewPrivs:[[:space:]]*1' "/proc/$pid/status" || fail "the service runs without NoNewPrivileges"
    grep -q '^CapEff:[[:space:]]*0000000000000000' "/proc/$pid/status" || fail "the service has capabilities"
    exposure=$(systemd-analyze security --no-pager dens@main.service 2>/dev/null |
        sed -n 's/.*Overall exposure level for dens@main.service: \([0-9.]*\).*/\1/p')
    printf 'systemd-analyze exposure: %s\n' "${exposure:-unknown}"
    awk -v e="$exposure" 'BEGIN { exit !(e != "" && e + 0 <= 1.5) }' ||
        fail "systemd-analyze rates the unit ${exposure:-unknown}; keep it at 1.5 or below"

    step "pair a browser and set the local password"
    as_alice dens status | grep -q "password:  not set" || fail "fresh install already has a password"
    url=$(as_alice dens open --print)
    token=${url#*#token=}
    api POST /api/pair "{\"token\":\"$token\"}" >/dev/null
    if api POST /api/pair "{\"token\":\"$token\"}" >/dev/null 2>&1; then
        fail "a pairing token worked twice"
    fi
    api POST /api/password "{\"password\":\"$PASSWORD\"}" >/dev/null
    api GET /api/status | grep -q '"passwordSet":true' || fail "the password isn't set after setup"
    api GET / | grep -q '<div id="app"' || fail "the page shell is missing"
    if curl -sS --fail -H "Host: attacker.example" "$CLIENT/" >/dev/null 2>&1; then
        fail "the client listener answered a foreign Host"
    fi

    step "an install hosts nothing until hosting is on, which dens den hosting turns on and off"
    as_alice dens status | grep -q "den:       not hosting a den" || fail "a fresh install hosts a den"
    ! den_answers 8485 || fail "the den listener answers with hosting off"
    if printf 'wrong wrong wrong\n' | as_alice dens den hosting on --password-stdin >/dev/null 2>&1; then
        fail "hosting turned on with the wrong password"
    fi
    printf '%s\n' "$PASSWORD" | as_alice dens den hosting on --password-stdin >/tmp/hosting.out ||
        fail "hosting didn't turn on: $(cat /tmp/hosting.out)"
    grep -q "127.0.0.1:8485" /tmp/hosting.out || fail "turning hosting on doesn't say where Caddy sends the den: $(cat /tmp/hosting.out)"
    den_answers 8485 || fail "the den listener doesn't answer with hosting on"
    as_alice dens status | grep -q "den:       hosting a den" || fail "dens status doesn't say the instance hosts"
    api GET /api/hosting | grep -q '"enabled":true,"serving":true' || fail "the page doesn't see hosting on: $(api GET /api/hosting)"
    as_alice dens den hosting off --yes | grep -q "Hosting is off." || fail "hosting didn't turn off"
    ! den_answers 8485 || fail "the den listener answers after hosting turned off"
    api GET /api/hosting | grep -q '"enabled":false,"serving":false' || fail "the page doesn't see hosting off: $(api GET /api/hosting)"

    step "another local user is refused"
    if runuser -u mallory -- dens status >/tmp/mallory.out 2>&1; then
        fail "mallory reached the control endpoint"
    fi
    grep -q "only answers alice" /tmp/mallory.out || fail "unexpected refusal: $(cat /tmp/mallory.out)"

    step "back up as alice"
    cd /home/alice
    printf '%s\n' "$PASSWORD" | as_alice dens backup --password-stdin -o /home/alice/alice.backup
    cp /home/alice/alice.backup /root/alice.backup
    printf 'E2E INSTALL PHASE PASSED\n'
}

phase_after_reboot() {
    step "the service and vault survive a reboot"
    for _ in $(seq 1 60); do
        systemctl is-active --quiet dens@main && break
        sleep 1
    done
    systemctl is-active --quiet dens@main || fail "dens@main didn't start at boot"
    as_alice dens status | grep -q "password:  set" || fail "the password is gone after reboot"
    api GET /api/status | grep -q '"passwordSet":true' || fail "the browser session didn't survive the reboot"

    version=$V1
    if [ -n "$V2" ]; then
        step "update through dens update"
        dens update --release-url file:///release/next/ --skip-verify
        expect_state main "$V2"
        as_alice dens status | grep -q "version:   $V2" || fail "the service didn't come back as $V2"
        as_alice dens status | grep -q "password:  set" || fail "the update lost the vault"
        version=$V2

        step "an older release won't install over a newer one"
        if dens update --release-url file:///release/ --skip-verify >/tmp/older.out 2>&1; then
            fail "dens update took an older release"
        fi
        grep -q "older than this $V2" /tmp/older.out || fail "unexpected update refusal: $(cat /tmp/older.out)"
        if SUDO_UID=$(alice_uid) SUDO_USER=alice sh /release/install.sh --release-url file:///release/ --skip-verify \
            >/tmp/older.out 2>&1; then
            fail "the installer put $V1 over $V2"
        fi
        grep -q "newer than this $V1" /tmp/older.out || fail "unexpected install refusal: $(cat /tmp/older.out)"
        expect_state main "$V2"
        systemctl is-active --quiet dens@main || fail "the refusal stopped dens@main"
    fi
    source_dir=/release
    [ -z "$V2" ] || source_dir=/release/next

    step "a second instance that hosts a den"
    SUDO_UID=$(alice_uid) SUDO_USER=alice sh "$source_dir/install.sh" --release-url "file://$source_dir/" --skip-verify \
        --instance second --client-port 18484
    systemctl is-active --quiet dens@second || fail "dens@second is not running"
    expect_state second "$version"
    expect_state main "$version"
    url=$(as_alice dens open --print --instance second)
    second_api POST /api/pair "{\"token\":\"${url#*#token=}\"}" >/dev/null
    second_api POST /api/password "{\"password\":\"$PASSWORD\"}" >/dev/null
    printf '%s\n' "$PASSWORD" | as_alice dens den hosting on --password-stdin --instance second >/dev/null ||
        fail "hosting didn't turn on in the second instance"
    as_alice dens status --instance second | grep -q "den:       hosting a den" || fail "the second instance doesn't host a den"
    den_answers 8485 || fail "the den listener doesn't answer"

    step "restore a backup into the second instance"
    # A browser paired before the restore must pair again after it.
    second_api GET /api/status >/dev/null || fail "pairing the second instance failed"
    backup=/root/alice.backup
    if [ -f /root/previous.backup ]; then
        # The previous distro's backup: a restore on another machine.
        backup=/root/previous.backup
    fi
    if printf 'wrong wrong wrong\n' | dens restore "$backup" --instance second --password-stdin --yes >/dev/null 2>&1; then
        fail "restore accepted the wrong password"
    fi
    printf '%s\n' "$PASSWORD" | dens restore "$backup" --instance second --password-stdin --yes
    expect_state second "$version"
    as_alice dens status --instance second | grep -q "password:  set" || fail "the restored instance has no password"
    if curl -sS --fail -b /root/cookies-second "$SECOND/api/status" >/dev/null 2>&1; then
        fail "a browser paired before the restore is still paired"
    fi
    # Hosting is the machine's choice, not the backup's (M9.1).
    as_alice dens status --instance second | grep -q "den:       not hosting a den" || fail "the restored instance hosts a den"
    ! den_answers 8485 || fail "the den listener answers after the restore"

    step "uninstall both instances"
    dens uninstall --instance second --yes
    [ -x /usr/local/bin/dens ] || fail "uninstalling one instance removed the shared binary"
    [ -e /etc/xdg/autostart/chat.dens.Dens.desktop ] || fail "uninstalling one instance removed the shortcut helper's autostart entry"
    dens uninstall --yes
    expect_uninstalled
    printf 'E2E AFTER-REBOOT PHASE PASSED\n'
}

# phase_installer installs a release built from another commit through
# this tree's installer, as the release currently offered is while new
# installers are published: the installer verifies the release's signature
# with cosign, the service starts and answers as that release, and
# uninstalling takes it all away.
phase_installer() {
    prepare
    step "install $V1 through the installer, verifying it"
    SUDO_UID=$(alice_uid) SUDO_USER=alice sh /release/install.sh --release-url file:///release/
    systemctl is-active --quiet dens@main || fail "dens@main is not running"
    expect_state main "$V1"
    as_alice dens status | grep -q "version:   $V1" || fail "the service doesn't answer as $V1"
    [ -x /usr/local/lib/dens/cosign ] || fail "the installer didn't keep the cosign it verified with"

    step "uninstall"
    dens uninstall --yes
    expect_uninstalled
    printf 'E2E INSTALLER PHASE PASSED\n'
}

case ${1:-} in
    install) phase_install ;;
    after-reboot) phase_after_reboot ;;
    installer) phase_installer ;;
    *) printf 'usage: %s install|after-reboot|installer\n' "$0" >&2; exit 2 ;;
esac
