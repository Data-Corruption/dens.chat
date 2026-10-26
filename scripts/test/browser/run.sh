#!/usr/bin/env bash
# Runs a browser check in the container setup.sh made, with this
# directory's scripts as they are now: CHECK's prep, then its driver, which
# prints OK or BAD for each thing it checks and exits nonzero on any BAD.
# Screenshots land in tester's home, named shot-<check>-*.png.
#
# Usage: scripts/test/browser/run.sh CHECK [ARGS...]
# The checks, by milestone, and the arguments their drivers take:
#   calls BROWSER BROWSER       M2, M3: two members in a call
#   youtube BROWSER BROWSER     M4.1: YouTube's players
#   share                       M4.2: screen shares, Chromium and Firefox
#   two-shares                  M4.2: two shares in a player, one the probe's
#   retention                   M5.1: retention, Chromium and Firefox
#   files                       M5.2: managing files, Chromium and Firefox
#   photos BROWSER              M5.3: smaller photos and the viewer
#   videos BROWSER              M5.4: smaller videos
#   webcodecs BROWSER           M5.5: copies the page makes
#   cache BROWSER               M6.1: the cache of other dens' files
#   history BROWSER             M6.2: history read offline
#   keys BROWSER                M7.1: voice keys with the helper
#   desktop BROWSER             M7.2: a desktop that binds the keys
#   answers BROWSER             M8: answers reach the den without addresses
#   hostile BROWSER             M8: a channel of hostile messages
#   hosting BROWSER             M9.1: hosting turned on and off in the settings
#   move BROWSER                M10: moving the den to another computer from the settings,
#                               and removing it from this one once its server is gone
#   fixes BROWSER               M11.1: the small fixes: dialogs, a key's capture, the two tabs,
#                               the New divider, the message box, folding groups, a late-sound video
#   messages BROWSER            M11.2: each message's ⋯ menu by keyboard and touch, its delete
#                               dialog, and links to messages, in a den and from another
# BROWSER is chromium or firefox.

set -euo pipefail
cd "$(dirname "$0")/../../.."
CONTAINER=${CONTAINER:-dens-browsers}
[[ $# -ge 1 ]] || { sed -n '6,32p' "$0" >&2; exit 2; }
[[ -f scripts/test/browser/drive-$1.mjs ]] || { echo "no check named $1" >&2; exit 2; }
for file in scripts/test/browser/*; do
    incus file push --quiet --uid 1000 --gid 1000 --mode 0755 "$file" "$CONTAINER/home/tester/browser/$(basename "$file")"
done
exec incus exec "$CONTAINER" --user 1000 --group 1000 --cwd /home/tester \
    --env HOME=/home/tester --env XDG_RUNTIME_DIR=/run/user/1000 -- bash browser/check.sh "$@"
