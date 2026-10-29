#!/usr/bin/env bash
# The M12.3 check's den: a DM between alice and bob that both have
# checked, and the desktop's notification service, dunst on Xvfb's
# display, which keeps each notification it shows until it's taken back,
# and writes down what it showed in ~/notify-shown.txt. ~/notify-bin holds
# an xdg-open that writes down the address it's given, in
# ~/notify-opened.txt, for the helper's clicks to open.
# shellcheck source=scripts/test/browser/lib.sh
source "$(dirname "$0")/lib.sh"
fresh
general=$(channel general)
dm=$(checked_dm alice bob)

export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus
pkill -u "$(id -u)" -x dunst 2>/dev/null || true
pkill -u "$(id -u)" -f 'dens-dev shortcuts' 2>/dev/null || true
rm -f notify-shown.txt notify-opened.txt
mkdir -p notify notify-bin
cat >notify/dunstrc <<'DUNSTRC'
[global]
    follow = none
[urgency_low]
    timeout = 0
[urgency_normal]
    timeout = 0
[urgency_critical]
    timeout = 0
[record]
    appname = "Dens"
    script = ~/notify/record.sh
DUNSTRC
cat >notify/record.sh <<'RECORD'
#!/bin/sh
# dunst gives a script the app's name, the summary and the body.
printf '%s|%s|%s|%s\n' "$1" "$2" "$3" "$DUNST_DESKTOP_ENTRY" >>"$HOME/notify-shown.txt"
RECORD
cat >notify-bin/xdg-open <<'OPEN'
#!/bin/sh
printf '%s\n' "$1" >>"$HOME/notify-opened.txt"
OPEN
chmod +x notify/record.sh notify-bin/xdg-open
DISPLAY=:99 nohup dunst -config notify/dunstrc >dunst.log 2>&1 &
for _ in $(seq 50); do
    dunstctl count displayed >/dev/null 2>&1 && break
    sleep 0.2
done
echo "DEN=$DEN"
echo "GENERAL=$general"
echo "DM=$dm"
