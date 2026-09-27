#!/bin/sh

# Runs inside an e2e container as root. test-lifecycle-e2e.sh calls it
# twice, with a reboot in between:
#   lifecycle-guest.sh install        prepare, install, pair, back up
#   lifecycle-guest.sh after-reboot   check, update, second instance,
#                                     restore, uninstall
# Environment: V1 and V2 are the fixture versions; V2 is empty when the
# release has no newer version to update to.

set -eu

PASSWORD="correct horse battery staple"
CLIENT=http://127.0.0.1:8484
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

# api METHOD PATH [JSON] prints the response body and fails on an error status.
api() {
    if [ $# -ge 3 ]; then
        curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $CLIENT" \
            -H "Content-Type: application/json" -d "$3" "$CLIENT$2"
    else
        curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$1" -H "Origin: $CLIENT" "$CLIENT$2"
    fi
}

expect_state() {
    state=$(cat "/var/lib/dens/$1/control/state.json")
    printf '%s\n' "$state" | grep -q "\"phase\":\"ready\",\"version\":\"$2\"" ||
        fail "instance $1 state is $state, want ready $2"
}

expect_mode() {
    actual=$(stat -c '%U:%G %a' "$1")
    [ "$actual" = "$2" ] || fail "$1 is $actual, want $2"
}

phase_install() {
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

    step "install as alice through sudo"
    SUDO_UID=$(alice_uid) SUDO_USER=alice APP_SKIP_VERIFY=true APP_RELEASE_URL=file:///release/ \
        sh /release/install.sh
    systemctl is-active --quiet dens@main || fail "dens@main is not running"
    expect_state main "$V1"
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
    api GET / | grep -q "Dens is running" || fail "home page missing after setup"
    if curl -sS --fail -H "Host: attacker.example" "$CLIENT/" >/dev/null 2>&1; then
        fail "the client listener answered a foreign Host"
    fi

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
        APP_SKIP_VERIFY=true dens update --release-url file:///release/next/
        expect_state main "$V2"
        as_alice dens status | grep -q "version:   $V2" || fail "the service didn't come back as $V2"
        as_alice dens status | grep -q "password:  set" || fail "the update lost the vault"
        version=$V2
    fi
    source_dir=/release
    [ -z "$V2" ] || source_dir=/release/next

    step "a second instance that hosts a den"
    SUDO_UID=$(alice_uid) SUDO_USER=alice APP_SKIP_VERIFY=true APP_RELEASE_URL="file://$source_dir/" \
        sh "$source_dir/install.sh" --instance second --client-port 18484 --den
    systemctl is-active --quiet dens@second || fail "dens@second is not running"
    expect_state second "$version"
    expect_state main "$version"
    as_alice dens status --instance second | grep -q "den:       hosting a den" || fail "the second instance doesn't host a den"
    curl -sS --fail http://127.0.0.1:8485/healthz >/dev/null || fail "the den listener doesn't answer"

    step "restore a backup into the second instance"
    # A browser paired before the restore must pair again after it.
    second=http://127.0.0.1:18484
    url=$(as_alice dens open --print --instance second)
    curl -sS --fail-with-body -c /root/cookies-second -H "Origin: $second" -H "Content-Type: application/json" \
        -d "{\"token\":\"${url#*#token=}\"}" "$second/api/pair" >/dev/null
    curl -sS --fail -b /root/cookies-second "$second/api/status" >/dev/null || fail "pairing the second instance failed"
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
    if curl -sS --fail -b /root/cookies-second "$second/api/status" >/dev/null 2>&1; then
        fail "a browser paired before the restore is still paired"
    fi

    step "uninstall both instances"
    dens uninstall --instance second --yes
    [ -x /usr/local/bin/dens ] || fail "uninstalling one instance removed the shared binary"
    dens uninstall --yes
    for path in /var/lib/dens /usr/local/bin/dens /etc/systemd/system/dens@.service /etc/sysusers.d/dens-main.conf; do
        [ ! -e "$path" ] || fail "$path survived uninstall"
    done
    if id dens-main >/dev/null 2>&1; then
        fail "the dens-main account survived uninstall"
    fi
    printf 'E2E AFTER-REBOOT PHASE PASSED\n'
}

case ${1:-} in
    install) phase_install ;;
    after-reboot) phase_after_reboot ;;
    *) printf 'usage: %s install|after-reboot\n' "$0" >&2; exit 2 ;;
esac
