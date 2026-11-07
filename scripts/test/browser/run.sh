#!/usr/bin/env bash
# Runs a browser check in the container setup.sh made, with this
# directory's scripts as they are now: CHECK's prep, then its driver, which
# prints OK or BAD for each thing it checks and exits nonzero on any BAD.
# Screenshots land in tester's home, named shot-<check>-*.png.
#
# Usage: scripts/test/browser/run.sh CHECK [ARGS...]
# The checks, by milestone, and the arguments their drivers take:
#   calls BROWSER BROWSER       M2, M3: two members in a call
#   youtube BROWSER BROWSER     M4.1: YouTube's players, and a cover's title and channel as links (M14.1)
#   share                       M4.2: screen shares, Chromium and Firefox
#   two-shares                  M4.2: two shares in a player, one the probe's
#   retention                   M5.1: retention, Chromium and Firefox
#   files                       M5.2: managing files, Chromium and Firefox, and from M17.2 a photo
#                               sent as a spoiler in a channel and a DM, covered until clicked
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
#                               the New divider, the message box, folding groups, a late-sound video,
#                               and a thin video's full screen button (M14.3)
#   messages BROWSER            M11.2: each message's ⋯ menu by keyboard, and by touch through a long
#                               press (M13.2), its delete dialog, and links to messages, in a den and
#                               from another; a link underlined only on hover and focus (M14.1)
#   talk BROWSER                M11.3: push to talk ringing in silence for the call, a watched share
#                               in a window of its own and back, and the logs downloaded
#   callaudio BROWSER BROWSER   M12.2: a call played as Android's app plays it, through a context
#                               made once the microphone is open, and played again on resume
#   notify BROWSER              M12.3: a DM's notification from a tab in the background, its click,
#                               taken back once read, none on screen or muted; and with no tab open,
#                               the helper's through the desktop's notifications, and its click
#   sounds BROWSER              M14.2: the page's sounds in a call, as each change plays them, from
#                               a file of the member's own, quieted and off, and a DM's sound, once
#                               with the browser's popup, alone with DMs off, none muted or on screen
#   soundboard BROWSER          M14.3: sounds staff add from Manage, opened from the den's name, each
#                               prepared by the page, played in a call from the call's block, Home's box
#                               and the phone's bar, received by the other page, one at a time, its
#                               player's ring lit, the rest heard by who joins partway, 0 muting it
#   pictures BROWSER            M15.1: an animated GIF and an animated WebP cropped into each member's
#                               picture and banner, shown still or playing, row by row, as each page's
#                               Animations says, which starts at On hover where the system asks for less motion
#   emoji BROWSER               M15.2: a still emoji and an animated one added in Manage, written by name,
#                               picked and completed, with @ for members, shown on both pages, in a DM too,
#                               and large alone; the picker led by the most used, in a skin tone; On hover;
#                               a renamed one's old name as text, the den's count full, and one deleted
#   reactions BROWSER           M15.3: reactions from the smiley beside a message's ⋯, its menu's Add
#                               reaction, a chip's click and the row's +, through the picker; the chips'
#                               tips; a DM's, sealed, the den's emoji by name; staff's Remove reactions;
#                               a twenty-first refused; and the view held as a row above it wraps
#   polls BROWSER               M15.4: a poll written in the composer, voted on from both pages, a vote final
#                               and one changed and taken back, ended early, its voters in a count's tip; and
#                               each pair of rules, the totals and voters reaching each page only as allowed,
#                               by every frame and answer the page receives
#   codes BROWSER               M16.1: a DM opened at once and written at the same moment from both pages;
#                               codes compared both ways, from the DM's lock and the profile beside it, own
#                               digits and wrong ones refused; the mark on bob's laptop within its shortened
#                               hour; and bob starting over: the line where his keys changed, his mark gone,
#                               the hold, Send anyway, and his new code compared
#   groups BROWSER              M16.3: a group made from the DM list's + New group and named, each change a line
#                               in it; written in, a photo sent, read through its members' Dens; dave added from
#                               its members, reading from his joining on; renamed, the name only sealed in the
#                               den's database; bob removed by alice, who made it, his page reading nothing
#                               after; and carol and alice leaving
#   disappearing BROWSER        M16.4: a DM's time set from the profile beside it, its line and its header;
#                               a message under a time of seconds marked, then gone from both pages, bob's
#                               cache and the den's database, into its change log; a group's time set from
#                               its members, and its message gone from both pages; and with the den stopped,
#                               a message gone from bob's page and cache all the same
#   phone [MODE...] [-- PART...]  M13: every screen at 390 pixels, its picture and an audit of its
#                               layout, in Chromium as a phone, Firefox as a touch screen, and Firefox
#                               with a mouse (MODE chromium, firefox or firefox-mouse; all three unless
#                               given), failing on anything past the right edge, a control under 24
#                               pixels, and with a finger a list's row under 44 or a control only a hover
#                               shows; getting around the panes by swipes, taps and Back (M13.1); touch, a
#                               long press, a sheet's swipe down, three lines sent and a tip's tap
#                               (M13.2); and pages, the settings and Manage as lists of pages Back walks
#                               out of, the viewer swiped and zoomed, and a dialog's sheet swiped down
#                               (M13.3); and the call, its bar, the call's button held and tapped by its
#                               role, a member's volume in a sheet, a share docked, full screen and back,
#                               and leaving from the bar (M13.4); and the emoji picker's sheet, its search
#                               at its top as it scrolls, and :par and @al completed by taps (M15.2); and a
#                               message's sheet with its quick reactions, a tap reacting, the chip's tip by
#                               a long press, and the picker's sheet (M15.3); and the composer's + as a sheet,
#                               a poll written in it and sent, voted on by taps, and its count's tip (M15.4);
#                               comparing codes from a DM's lock (M16.1); and a DM's time from its header (M16.4);
#                               and Compact picked by a tap, a channel's messages a line each, a long press
#                               on one, and a spoiler covered until a tap shows it (M17.2); and the notes
#                               opened from the top of the Direct messages list, a note sent with nothing
#                               marked unread, and the own profile card's Notes (M17.3); and the events
#                               opened from the top of the Den tab, Interested marked and taken back by taps,
#                               and the owner's events, their form as a sheet and Cancel's dialog (M17.4);
#                               and an event's card under its link, Interested by taps, and a badge's tip
#                               shown by a tap on a profile card, within the screen (M17.5).
#                               PART is member, around, touch, pages, call, emoji, reactions, polls, compact,
#                               notes, events, owner or before; all unless given
#   site [MODE...]              M17.1: the public site's install guide, its choice of platform and each
#                               platform's page holding its own steps alone, its folds closed until opened,
#                               the Flatpak's old link landing on the choice, and the links into the guides
#                               reaching their anchors; at 390 pixels by touch and at a desktop's width with
#                               a mouse, with the phone check's audits (MODE chromium, firefox,
#                               chromium-desktop or firefox-desktop; all four unless given). setup.sh builds
#                               the site, so run it again after changing the site
#   drag BROWSER BROWSER        M17.2: alice, in the first, dragging a channel out of its group and onto
#                               another's heading, and a group above another, as the den then has them;
#                               and bob, in the second, in a call, muted, dragged onto another voice
#                               channel, whose call his page joins, sealed and still muted, and Move to in
#                               his menu taking him back
#   notes BROWSER               M17.3: bob's notes opened from the top of his Direct messages tab, the den
#                               making them then; a note with a photo written on his phone, the composer
#                               offering no one else's editing, poll or Spoiler; his laptop opening them from
#                               his own profile card's Notes, reading both and writing back, nothing marked
#                               unread; and the den's database holding neither in the clear, alice's Dens
#                               nothing of them
#   events BROWSER              M17.4: the Events row atop each Den tab, + Event for alice alone; an event she
#                               makes through the form reaching bob's page, his Interested counted on hers;
#                               one in a staff-only channel never reaching him; members let make events in
#                               Limits, bob making one somewhere else and canceling it; the event starting in seconds,
#                               his background tab's notification of it, whose click opens his events at it;
#                               Live on both pages, its end moved from the form, its start kept, then End now
#                               taking it off both and his notification back; one of seconds ending by the
#                               den's timer; and the den's database holding no name in the clear. M17.5: the
#                               event's badge from alice's form; its copied link in #general and their DM,
#                               a chip and its card on bob's page, his Interested from the card; alice
#                               earning the badge in its call, bob joining and leaving at once earning none;
#                               the badge and its tip on alice's profile card; the card saying Ended; and the
#                               den's database holding alice's award alone, the badge's title sealed
# BROWSER is chromium or firefox.

set -euo pipefail
cd "$(dirname "$0")/../../.."
CONTAINER=${CONTAINER:-dens-browsers}
[[ $# -ge 1 ]] || { sed -n '6,136p' "$0" >&2; exit 2; }
[[ -f scripts/test/browser/drive-$1.mjs ]] || { echo "no check named $1" >&2; exit 2; }
for file in scripts/test/browser/*; do
    incus file push --quiet --uid 1000 --gid 1000 --mode 0755 "$file" "$CONTAINER/home/tester/browser/$(basename "$file")"
done
exec incus exec "$CONTAINER" --user 1000 --group 1000 --cwd /home/tester \
    --env HOME=/home/tester --env XDG_RUNTIME_DIR=/run/user/1000 -- bash browser/check.sh "$@"
