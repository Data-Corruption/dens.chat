#!/usr/bin/env bash
# Question 7 here: the player page in the container's Chromium and Firefox,
# each in a window in front, in a tab behind another, and in a minimized
# window, on a real X display (Xvfb with a window manager) and PulseAudio's
# null sink, fed a tone from the other container across the link. The
# front pages run a second time with the page's own thread carrying the
# sound (via=main).
#
# Usage: spikes/relay/run/players.sh [SECONDS]     1800 unless given
#
# The receiving probe's log, relay/logs/here/players-a.log, holds what each
# page's worklet counted, by the page's name.

set -euo pipefail
cd "$(dirname "$0")/../.."
A=dens-browsers
B=dens-relay
SECS=${1:-1800}
LOGS=relay/logs/here
mkdir -p "$LOGS"
ip_of() { incus exec "$1" -- ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1; }
IPA=$(ip_of $A)
IPB=$(ip_of $B)

cat >"$LOGS/players-inside.sh" <<'EOF'
#!/usr/bin/env bash
# Runs in the container, as tester: the display, the browsers, their places.
set -u
SECS=$1
export DISPLAY=:98 XDG_RUNTIME_DIR=/run/user/1000
URL='http://127.0.0.1:47880/'
pids=()
Xvfb :98 -screen 0 3200x1400x24 >/dev/null 2>&1 &
pids+=($!)
sleep 1
openbox >/dev/null 2>&1 &
pids+=($!)
sleep 1

declare -A pid
chromium_start() { # NAME X Y QUERY
  rm -rf "/tmp/relay-$1"
  chromium --user-data-dir="/tmp/relay-$1" --no-first-run --no-default-browser-check --no-sandbox \
    --autoplay-policy=no-user-gesture-required --window-position="$2,$3" --window-size=700,500 \
    "$URL?auto=1&name=$1$4" >/dev/null 2>&1 &
  pid[$1]=$!
}
firefox_start() { # NAME X Y QUERY
  rm -rf "/tmp/relay-$1"
  mkdir -p "/tmp/relay-$1"
  cat >"/tmp/relay-$1/user.js" <<'PREFS'
user_pref("media.autoplay.default", 0);
user_pref("media.autoplay.blocking_policy", 0);
user_pref("browser.shell.checkDefaultBrowser", false);
user_pref("browser.aboutwelcome.enabled", false);
user_pref("datareporting.policy.dataSubmissionEnabled", false);
user_pref("toolkit.telemetry.reportingpolicy.firstRun", false);
user_pref("browser.startup.homepage_override.mstone", "ignore");
user_pref("app.update.enabled", false);
PREFS
  firefox-esr --no-remote --profile "/tmp/relay-$1" --width 700 --height 500 "$URL?auto=1&name=$1$4" >/dev/null 2>&1 &
  pid[$1]=$!
}
window() { xdotool search --onlyvisible --pid "${pid[$1]}" 2>/dev/null | tail -n 1; }

chromium_start chromium-front 0 0 ''
chromium_start chromium-behind 780 0 ''
chromium_start chromium-min 1560 0 ''
chromium_start chromium-front-main 2340 0 '&via=main'
firefox_start firefox-front 0 600 ''
firefox_start firefox-behind 780 600 ''
firefox_start firefox-min 1560 600 ''
firefox_start firefox-front-main 2340 600 '&via=main'
sleep 20
x=0
for name in firefox-front firefox-behind firefox-min firefox-front-main; do
  xdotool windowmove "$(window $name)" $x 600
  x=$((x + 780))
done
sleep 2
# A new tab in front of the player's, as Ctrl+T opens one.
for name in chromium-behind firefox-behind; do
  xdotool windowactivate --sync "$(window $name)" key --clearmodifiers ctrl+t
  sleep 1
done
for name in chromium-min firefox-min; do
  xdotool windowminimize "$(window $name)"
done
echo "windows placed at $(date +%T)"
sleep "$SECS"
for name in "${!pid[@]}"; do kill "${pid[$name]}" 2>/dev/null; done
sleep 2
kill "${pids[@]}" 2>/dev/null
EOF
incus file push -q "$LOGS/players-inside.sh" $A/home/tester/relay-players.sh
incus exec $A -- chown tester:tester /home/tester/relay-players.sh

total=$((SECS + 45))
incus exec $B -- sh -c "rm -f /tmp/players-b.log; exec /usr/local/bin/relayprobe -quiet -log /tmp/players-b.log -peer $IPA -role b -page off -send tone -for ${total}s" &
incus exec $A -- sh -c "rm -f /tmp/players-a.log; exec /usr/local/bin/relayprobe -quiet -log /tmp/players-a.log -peer $IPB -role a -page 127.0.0.1:47880 -for ${total}s" &
sleep 3
incus exec $A -- su - tester -c "bash /home/tester/relay-players.sh $SECS"
wait
incus file pull -q $A/tmp/players-a.log "$LOGS/players-a.log"
incus file pull -q $B/tmp/players-b.log "$LOGS/players-b.log"
relay/run/players-summary.sh "$LOGS/players-a.log"
