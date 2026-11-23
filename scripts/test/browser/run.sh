#!/usr/bin/env bash
# Runs a browser check in the container setup.sh made, with this
# directory's scripts as they are now: CHECK's prep, then its driver, which
# prints OK or BAD for each thing it checks and exits nonzero on any BAD.
# Screenshots land in tester's home, named shot-<check>-*.png. The page is
# in the development binary setup.sh pushed, so a change to the page needs
# setup.sh again, and instances already running serve the binary they
# started with until a check's prep starts them afresh. The checks share
# those instances: run one at a time.
#
# Usage: scripts/test/browser/run.sh CHECK [ARGS...]
# The checks, and the arguments their drivers take:
#   calls BROWSER BROWSER       two members in a call, in its pane's tiles, and away on both
#                               pages; staff sliding a member's
#                               volume with the mouse, their menu staying put; and a member at 200%,
#                               measured twice as loud as their stream; and the call's length on
#                               both pages a second apart, in the rainbow past a short record with
#                               fireworks, and none of it with the timer off
#   youtube BROWSER BROWSER     YouTube's players, and a cover's title and channel as links,
#                               and a Short's cover and player portrait
#   share                       screen shares, Chromium and Firefox; a share turned down,
#                               muted, unmuted and watched again, playing where it was
#   two-shares                  two shares on the call's pane's stage, then in a player, one the probe's
#   retention                   retention, Chromium and Firefox
#   files                       managing files, Chromium and Firefox, and a photo
#                               sent as a spoiler in a channel and a DM, covered until clicked; and
#                               a message's GIF far up a channel, unfetched until it nears the screen, playing
#                               there under Always, and under the pointer with On hover
#   photos BROWSER              smaller photos and the viewer
#   videos BROWSER              smaller videos
#   webcodecs BROWSER           copies the page makes
#   cache BROWSER               the cache of other dens' files
#   history BROWSER             history read offline
#   keys BROWSER                voice keys with the helper
#   desktop BROWSER             a desktop that binds the keys, toggle deafen among them
#   answers BROWSER             answers reach the den without addresses
#   hostile BROWSER             a channel of hostile messages
#   hosting BROWSER             hosting turned on and off in the settings
#   move BROWSER                moving the den to another computer from the settings,
#                               and removing it from this one once its server is gone
#   fixes BROWSER               the small fixes: dialogs, a key's capture, the two tabs,
#                               the New divider, the message box, folding groups, a late-sound video,
#                               and a thin video's full screen button; and the crown and the
#                               shield at one height, a long name scrolling under the pointer, and
#                               right clicks on a message, a link and a member; and the message
#                               bar as its box, the box growing with wrapped lines to half its pane, a
#                               reaction's chip square on a square theme, and file names on hover
#   switcher BROWSER            Ctrl+K pressed as keys, the switcher listing the unread and opening a
#                               channel in another den by a few letters and back; Alt+↓ and Alt+↑ past
#                               a voice channel; Esc marking a channel scrolled up in read, at its newest;
#                               Mark all as read; a member's channel menu copying its link; and a
#                               channel's link completed from #, opened by alice, and #unknown to bob
#                               for a staff-only channel; and a voice channel's Enter opening its call's
#                               pane, from a channel, which Back returns to, and from Home
#   messages BROWSER            each message's ⋯ menu by keyboard, and by touch through a long
#                               press, its delete dialog, and links to messages, in a den and
#                               from another; a link underlined only on hover and focus
#   talk BROWSER                push to talk ringing in silence for the call, a watched share
#                               in a window of its own and back, and the logs downloaded; and a
#                               release delay holding the gate and the rings open after the key's
#                               release, and the deafen key
#   callaudio BROWSER BROWSER   a call played as Android's app plays it, through a context
#                               made once the microphone is open, and played again on resume
#   notify BROWSER              a DM's notification from a tab in the background, its click,
#                               taken back once read, none on screen or muted; and with no tab open,
#                               the helper's through the desktop's notifications, and its click
#   audit BROWSER               alice, in Chromium, finding carol in Manage's Members by a few letters and
#                               promoting her from her row's menu, removing dave from his, and deleting bob's
#                               message and a channel; carol, a moderator, in BROWSER, reading each line in
#                               Manage's Audit log, nothing of the message's words; and bob, a member, offered
#                               neither section and refused the log
#   sounds BROWSER              the page's sounds in a call, as each change plays them, from
#                               a file of the member's own, quieted and off, and a DM's sound, once
#                               with the browser's popup, alone with DMs off, none muted or on screen
#   soundboard BROWSER          sounds staff add from Manage, opened from the den's name's menu, each
#                               prepared by the page, played in a call from the call's block, Home's box
#                               and the phone's bar, received by the other page, one at a time, its
#                               player's ring lit, the rest heard by who joins partway, 0 muting it
#   pictures BROWSER            an animated GIF and an animated WebP cropped into each member's
#                               picture and banner, shown still or playing, row by row, as each page's
#                               Animations says, which starts at On hover where the system asks for less motion;
#                               and bob's a transparent GIF, its preview hidden once it plays
#   emoji BROWSER               a still emoji and an animated one added in Manage, written by name,
#                               picked and completed, with @ for members, shown on both pages, in a DM too,
#                               and large alone; the picker led by the most used, in a skin tone; On hover;
#                               a renamed one's old name as text, the den's count full, and one deleted
#   reactions BROWSER           reactions from the smiley beside a message's ⋯, its menu's Add
#                               reaction, a chip's click and the row's +, through the picker; the chips'
#                               tips; a DM's, sealed, the den's emoji by name; staff's Remove reactions;
#                               a twenty-first refused; and the view held as a row above it wraps
#   polls BROWSER               a poll written in the composer, voted on from both pages, a vote final
#                               and one changed and taken back, ended early, its voters in a count's tip; and
#                               each pair of rules, the totals and voters reaching each page only as allowed,
#                               by every frame and answer the page receives
#   codes BROWSER               a DM opened at once and written at the same moment from both pages;
#                               codes compared both ways, from the DM's lock and the profile beside it, own
#                               digits and wrong ones refused; the mark on bob's laptop within its shortened
#                               hour; and bob starting over: the line where his keys changed, his mark gone,
#                               the hold, on alice's laptop too, signed in as it holds, Send anyway lifting
#                               it on both, and his new code compared
#   groups BROWSER              a group made from the DM tab's + and named, each change a line
#                               in it; written in, a photo sent, read through its members' Dens; dave added from
#                               its members, reading from his joining on; renamed, the name only sealed in the
#                               den's database; bob removed by alice, who made it, his page reading nothing
#                               after; and carol and alice leaving
#   disappearing BROWSER        a DM's time set from the profile beside it, its line and its header;
#                               messages a moment apart either side of a change of the time, each
#                               with its clock;
#                               a message under a time of seconds marked, then gone from both pages, bob's
#                               cache and the den's database, into its change log; a group's time set from
#                               its members, and its message gone from both pages; and with the den stopped,
#                               a message gone from bob's page and cache all the same
#   phone [MODE...] [-- PART...]  every screen at 390 pixels, its picture and an audit of its
#                               layout, in Chromium as a phone, Firefox as a touch screen, and Firefox
#                               with a mouse (MODE chromium, firefox or firefox-mouse; all three unless
#                               given), failing on anything past the right edge, a control under 24
#                               pixels, and with a finger a list's row under 44 or a control only a hover
#                               shows; getting around the panes by swipes, taps and Back, the switcher
#                               from its button, and a member's channel menu by a long press; touch, a
#                               long press, a sheet's swipe down, three lines sent and a tip's tap;
#                               and pages, the settings and Manage as lists of pages Back walks
#                               out of, the viewer swiped and zoomed, and a dialog's sheet swiped down;
#                               and the call, its pane from the bar's tap, its bar, the call's button held and tapped by its
#                               role, a member's volume in a sheet, a share docked, full screen and back,
#                               and leaving from the bar; and the emoji picker's sheet, its search
#                               at its top as it scrolls, and :par and @al completed by taps; and a
#                               message's sheet with its quick reactions, a tap reacting, the chip's tip by
#                               a long press, and the picker's sheet; and the composer's + as a sheet,
#                               a poll written in it and sent, voted on by taps, and its count's tip;
#                               comparing codes from a DM's lock; and a DM's time from its header;
#                               and Compact picked by a tap, a channel's messages a line each, a long press
#                               on one, and a spoiler covered until a tap shows it; and the notes
#                               opened from the top of the Direct messages list, a note sent with nothing
#                               marked unread, and the own profile card's Notes; and the events
#                               opened from the top of the Den tab, Interested marked and taken back by taps,
#                               and the owner's events, their form as a sheet and Cancel's dialog;
#                               and an event's card under its link, Interested by taps, and a badge's tip
#                               shown by a tap on a profile card, within the screen; and a profile
#                               card's ⋯ as a sheet, Remove from den… reached from it; and the
#                               theme editor and the chat background's card with its Dim; and Edit
#                               profile's Name style, a font and a gradient picked; and Staff marks
#                               and the background's Hover; and Call timer, and a call's length at
#                               its row's right, its lock behind the call's block's arrow; and the den's
#                               own sections from the cog on its page, Manage's pages headed by their names, and
#                               every button one of the page's kinds, a dialog leading with one primary;
#                               and before's first run asking about Link previews, and a Steam link's card;
#                               and Appearance as a page, each tab along its foot and each of its editor's.
#                               PART is member, around, touch, pages, call, emoji, reactions, polls, compact,
#                               notes, events, owner or before; all unless given
#   site [MODE...]              the public site's install guide, its choice of platform and each
#                               platform's page holding its own steps alone, its folds closed until opened,
#                               the Flatpak's old link landing on the choice, and the links into the guides
#                               reaching their anchors; at 390 pixels by touch and at a desktop's width with
#                               a mouse, with the phone check's audits (MODE chromium, firefox,
#                               chromium-desktop or firefox-desktop; all four unless given). setup.sh builds
#                               the site, so run it again after changing the site
#   drag BROWSER BROWSER        alice, in the first, dragging a channel out of its group and onto
#                               another's heading, and a group above another, as the den then has them;
#                               and bob, in the second, in a call, muted, dragged onto another voice
#                               channel, whose call his page joins, sealed and still muted, and Move to in
#                               his menu taking him back; and carol, a moderator, offered Move to on bob, a
#                               moderator, without Mute for everyone or Disconnect, dragging him
#   notes BROWSER               bob's notes opened from the top of his Direct messages tab, the den
#                               making them then; a note with a photo written on his phone, the composer
#                               offering no one else's editing, poll or Spoiler; his laptop opening them from
#                               his own profile card's Notes, reading both and writing back, nothing marked
#                               unread; and the den's database holding neither in the clear, alice's Dens
#                               nothing of them
#   events BROWSER              the Events row atop each Den tab, + Event for alice alone; an event she
#                               makes through the form reaching bob's page, his Interested counted on hers;
#                               one in a staff-only channel never reaching him; members let make events in
#                               Limits, bob making one somewhere else and canceling it; the event starting in seconds,
#                               his background tab's notification of it, whose click opens his events at it;
#                               Live on both pages, its end moved from the form, its start kept, then End now
#                               taking it off both and his notification back; one of seconds ending by the
#                               den's timer; and the den's database holding no name in the clear. The
#                               event's badge from alice's form; its copied link in #general and their DM,
#                               a chip and its card on bob's page, his Interested from the card; alice
#                               earning the badge in its call, bob joining and leaving at once earning none;
#                               the badge and its tip on alice's profile card; the card saying Ended; and the
#                               den's database holding alice's award alone, the badge's title sealed; and
#                               Interested colored until marked, then plain with a tick
#   settings BROWSER [PART...]  every section of the settings and of Manage opened from the list down
#                               the side, with the layout audit, every hint in them reading in a line, and
#                               bob found in Manage's Members by two letters, his row's menu opened; and
#                               Appearance's sizes, a photo and a YouTube player each shown at the size
#                               chosen, a large photo drawn itself over its preview; and a theme of the member's
#                               own made in the editor, saved, edited, its code copied and pasted into bob's
#                               browser, a stranger's codes refused; and
#                               a picture set behind general's messages, found there under its dim by the
#                               screen's pixels, in a second browser too, and removed; and Staff
#                               marks, the message bar's hue on a green theme of her own, and the background's
#                               Hover in both browsers; and each section held to the one frame,
#                               its groups cards, one toggle size, regular labels, every button one of the page's
#                               kinds; Manage holding what staff manage; and her own sections of the den from the
#                               cog on its page; and the code a block, a stranger's blocks read for
#                               what the page reads alone, and a v1.13.0 code read; and Appearance
#                               as a page, Back to the section before, its preview following each setting, and
#                               at 1900, 1100 and 390 pixels a built-in theme chosen against the page's, worn
#                               by Use, copied by Customize, edited against the preview until Save, and
#                               DaisyUI's generator's block pasted, every variable set as it gives it.
#                               PART is sections, preview, sizes, themes, codes or background; all unless given
#   names BROWSER               alice picking a font and a gradient in Edit profile, its fonts as
#                               tiles in their own faces; bob's page showing her name in them in the member
#                               list, a message, a reply's quote, a call, the DM list, a DM's header and her
#                               card, the dark end lightened on Dark and the pale one darkened on Light, by
#                               the page's styles and the screen's pixels, no row taller, her font alone
#                               fetched; a Cyrillic name in the page's font; bob's Name styles at Colors
#                               only and Off; each font's letters inside a name's box; and alice
#                               clearing her style
#   relay BROWSER               bob in a call on his first install, his laptop's install linked to it
#                               through the den's introduction, its stand-in helper's push to talk ringing
#                               him on alice's page, its toggle mute muting him once, the ring out within half
#                               a second of the laptop's install stopping; his Relay settings listing the
#                               laptop as linked; and the laptop, with its real helper, sharing the
#                               container's sound, a tone, which his first page plays from Listen, at the tone's
#                               level and never dry, behind another tab too, by the worklet's counts; the bars
#                               on both ends; a second page taking over, the first told; the laptop's Stop; and
#                               no address in the den's database or his installs' logs; and the
#                               laptop's page, as a phone's, holding the call's button for his call, under the
#                               name of the device holding it, which rings him on alice's page while it's held;
#                               and his Home holding his bar and his call in one box
#   signin BROWSER              bob's laptop signing in from its page, saying at once that it waits for
#                               his other device; his first device, at a phone's width, showing the request on
#                               each pane; the digits typed both ways, and the laptop signed in
#   steam BROWSER               Steam's cards against a stand-in for Steam: off until bob turns them on,
#                               asking nothing; each game once, three to a message, none in code or a
#                               spoiler; a sale's card in full, held to its size as it loads, its arrows
#                               stepping through the screenshots; a free game, one coming, one Steam lacks;
#                               and one for adults only, covered and unasked until shown
#   review [PASS...] [-- PART...]  every screen for a review of the page, in Chromium at 1280 by 800
#                               with a mouse and at 390 by 844 by touch, each on Dark and on Light: the phone
#                               check's screens and those it lacks, a call with a share, the owner's, the first
#                               run, and a second device signing in. It checks only that each was reached; the
#                               pictures land in ~/shot-review/PASS. PASS is desk-dark, desk-light, phone-dark
#                               or phone-light; PART is member, more, call, owner, before or signin
# BROWSER is chromium or firefox.

set -euo pipefail
cd "$(dirname "$0")/../../.."
CONTAINER=${CONTAINER:-dens-browsers}
[[ $# -ge 1 ]] || { awk '/^# Usage:/ { on = 1 } on && !/^# / { exit } on' "$0" >&2; exit 2; }
[[ -f scripts/test/browser/drive-$1.mjs ]] || { echo "no check named $1" >&2; exit 2; }
for file in scripts/test/browser/*; do
    incus file push --quiet --uid 1000 --gid 1000 --mode 0755 "$file" "$CONTAINER/home/tester/browser/$(basename "$file")"
done
exec incus exec "$CONTAINER" --user 1000 --group 1000 --cwd /home/tester \
    --env HOME=/home/tester --env XDG_RUNTIME_DIR=/run/user/1000 -- bash browser/check.sh "$@"
