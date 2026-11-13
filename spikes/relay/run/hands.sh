#!/usr/bin/env bash
# The Relay spike's part that takes hands and ears (questions 6, 8 and 9):
# it walks whoever holds the phone through each step, works the phone and
# the Windows probe itself, and keeps what they answer, with Android's own
# account of the app's sound at each step, in relay/logs/phone/hands.txt.
#
# Usage: spikes/relay/run/hands.sh [SERIAL]     10.0.0.33:33669 unless given
#
# The Windows probe must be sending already (link.ps1 with -Page, or the
# session's own run): its page at 127.0.0.1:47880 holds the click page.

set -euo pipefail
cd "$(dirname "$0")/../.."
S=${1:-10.0.0.33:33669}
APP=chat.dens.Dens.dev
WIN='C:\Users\datac\dens-vm\relay'
WINDIR=/mnt/c/Users/datac/dens-vm/relay
OUT=${OUT:-relay/logs/phone/hands.txt}
# For trying the script out on the emulator: its build, shorter steps, and
# no recordings.
ARCH=${ARCH:-arm64}
STEP=${STEP:-30}
DRY=${DRY:-}
mkdir -p relay/logs/phone
# Nothing the script runs may read the terminal: the answers are the reader's.
adb() { incus exec dens-android -- /opt/android/sdk/platform-tools/adb -s "$S" "$@" </dev/null; }

note() { printf '%s\n' "$*" >>"$OUT"; }
say() { printf '\n\033[1m%s\033[0m\n' "$*"; note "## $(date +%T) $*"; }
enter() { read -r -p "    Press Enter when that's done. " _; }
ask() {
  printf '\n\033[1m%s\033[0m\n' "$1"
  read -r -p "    > " answer
  note "Q: $1"
  note "A: $answer"
}
sample() { relay/run/phone.sh "$S" audio </dev/null 2>&1 | sed "1s/now/$1/" >>"$OUT"; }
relay() { relay/run/phone.sh "$S" relay "-peer 10.0.0.43 -role b -keys synth" "page=$1" </dev/null >/dev/null 2>&1 || :; }
calls() { adb shell "run-as $APP sh -c 'grep -c \"kind.:.call\" files/shell/relay.log'" 2>/dev/null | tr -d '\r' || echo 0; }

# clicks NAME: the Windows microphone listens for 20 seconds.
clicks() {
  [[ -z $DRY ]] || return 0
  while :; do
    (cd "$WINDIR" && cmd.exe /c "$WIN\\relayprobe.exe clicks -for 20s -wav $WIN\\logs\\clicks-$1.wav" </dev/null 2>&1 | tr -d '\r') | tee -a "$OUT"
    read -r -p "    Enter to go on, or r and Enter to record again. " again
    [[ $again == r ]] || break
  done
}

# walk NAME: the page opens the microphone as a call does, and makes its
# player again, step by step; each step is said as the page reports it.
walk() {
  local name=$1 seen=0 now i
  local steps=(
    "NOW the microphone is open, as when you join a call. The relay is the one that was already playing."
    "NOW the relay has started again, inside the call. (It was silent for 8 seconds on purpose.)"
    "NOW the microphone is closed, as when the call ends. The relay is the one started inside the call."
    "NOW the relay has started again, after the call. (Silent for 8 seconds on purpose again.) That was the last step."
  )
  relay "auto=1&name=$name&call=$STEP"
  echo "    The relay plays as before for $STEP seconds; then four things happen, about $STEP seconds apart. Listen at each."
  for ((i = 0; i < 100 && seen < 4; i++)); do
    sleep 2
    now=$(calls)
    if [[ $now =~ ^[0-9]+$ ]] && ((now > seen)); then
      seen=$now
      printf '\n    \033[1m%s\033[0m\n' "${steps[seen - 1]}"
      note "## $(date +%T) $name: ${steps[seen - 1]}"
      sleep 4
      sample "$name step $seen"
    fi
  done
  ((seen == 4)) || echo "    (The phone reported only $seen of the four steps.)"
  sleep 20
  sample "$name end"
  relay/run/phone.sh "$S" pull "$name" </dev/null >/dev/null 2>&1 || :
}

note ""
note "# $(date '+%F %T') hands.sh"
if [[ -z $DRY ]] && ! /mnt/c/Windows/System32/tasklist.exe 2>/dev/null | grep -qi relayprobe.exe; then
  echo "The Windows probe isn't running: starting one for two hours."
  (cd "$WINDIR" && nohup cmd.exe /c "$WIN\\relayprobe.exe -quiet -peer ${S%%:*} -role a -send system -keys F8,F9 -page 127.0.0.1:47880 -for 120m -log $WIN\\logs\\win-phone-hands.log" >/dev/null 2>&1 &)
  sleep 3
fi

echo "Putting the newest build on the phone..."
(cd .. && scripts/test/android/phone.sh "$S" install "$ARCH" </dev/null 2>&1 | tail -n 1)
adb shell "pm grant $APP android.permission.RECORD_AUDIO; pm grant $APP android.permission.ACCESS_LOCAL_NETWORK; pm grant $APP android.permission.POST_NOTIFICATIONS" || :
(cd .. && scripts/test/android/phone.sh "$S" start </dev/null)

say "1 of 7. Unlock the phone, with nothing connected to it, and leave \"Dens (development)\" in front. Play music on the PC, and leave it playing."
enter
relay "auto=1&name=phone-speaker"
sleep 15
sample "speaker, screen on"
ask "On the phone's speaker, for a minute: any gaps or crackles? How does it sound?"
ask "Press the phone's volume buttons. Which volume does Android show, and does it change how loud the relay is?"
ask "Start some sound in another app on the phone, a video say. Do the two play together? Does either get quieter, or stop? (Stop the other app's sound after.)"
relay/run/phone.sh "$S" pull phone-speaker </dev/null >/dev/null 2>&1 || :

say "2 of 7. The delay on the speaker. Pause the music on the PC. In a browser on Windows open http://127.0.0.1:47880/click.html and click \"Start clicking\": the PC clicks every two seconds, and the phone clicks after it. Put the PC's microphone where it hears both the PC's own click and the phone's speaker."
enter
clicks speaker

say "3 of 7. Connect your Bluetooth headphones to the phone. Leave the clicking on."
enter
sleep 5
sample "bluetooth, screen on"
echo "    Now the same recording: the microphone must hear the PC's own click and one earcup of the headphones."
enter
clicks bluetooth

say "4 of 7. Close the clicking tab and play music on the PC again. Keep the headphones on."
enter
ask "On the headphones, for a minute: any gaps or crackles? How does it sound, and how far behind the PC is it?"

say "5 of 7. A call beside it, on Bluetooth. The phone opens its microphone as it does when you join a call, then closes it. Keep the headphones on and the phone unlocked, and listen."
walk phone-call-bluetooth
ask "On the headphones, what happened to the sound at each of the four moments? (Quieter, louder, silent, moved to the phone's speaker, worse quality, nothing?)"

say "6 of 7. The same on the phone's speaker. Disconnect the Bluetooth headphones, or switch them off."
enter
walk phone-call-speaker
ask "On the speaker, what happened to the sound at each of the four moments? (Quieter, louder, silent, moved to the earpiece, nothing?)"

say "7 of 7. Wired headphones, if you have some that plug into the phone. Plug them in (or type s to skip)."
read -r -p "    Enter when plugged in, or s and Enter to skip. " wired
if [[ $wired != s ]]; then
  relay "auto=1&name=phone-wired"
  sleep 10
  sample "wired, screen on"
  echo "    Pause the music, start the clicking page again, and let the microphone hear the PC's click and one of the wired earpieces."
  enter
  clicks wired
  echo "    Close the clicking tab. Unplug the wired headphones when you like."
fi

relay "auto=1&name=phone&buffer=150"
say "Done. Last thing: connect the Bluetooth headphones again if you can spare them for an hour, lock the phone, and leave it lying. Then tell Claude: the next hour runs by itself, on the headphones, with the screen off."
echo "    Your answers are in spikes/$OUT."
