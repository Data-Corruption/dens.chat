#!/usr/bin/env bash

# Makes the speech the RNNoise spike's page cleans: 60 seconds of one voice
# and 67 of another, from Windows' text to speech at 48 kHz, from WSL. Only
# the page's quality checks need them.
#
# Usage: spikes/rnnoise/tts.sh   (from the repository's root)
# Output: out/spikes/rnnoise/{speech,voices}.wav

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
out=out/spikes/rnnoise
win_temp=$(powershell.exe -NoProfile -Command '[IO.Path]::GetTempPath()' | tr -d '\r')
work=$(wslpath -u "${win_temp}rnnoise-tts")
rm -rf "$work"
mkdir -p "$work" "$out"
trap 'rm -rf "$work"' EXIT
cat >"$work/tts.ps1" <<'EOF'
Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
$names = @($s.GetInstalledVoices() | Where-Object { $_.Enabled } | ForEach-Object { $_.VoiceInfo.Name })
$fmt = New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo(48000, [System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen, [System.Speech.AudioFormat.AudioChannel]::Mono)
$speech = @"
We left the house a little after six, when the street was still grey and quiet. The plan was simple: drive out to the lake, find the old trail along the north shore, and be back before the weather turned. Nobody had checked the weather, of course. By the time we reached the trailhead the wind had picked up, and the water was the colour of slate. Still, the path was dry, the air smelled of pine, and for the first hour we hardly said a word. Somewhere past the second bridge, Sam stopped and pointed at a heron standing perfectly still in the shallows. It watched us for a long moment, then lifted off with three slow beats of its wings and was gone. We ate lunch on a flat rock above the water, sharing a thermos of coffee that had gone lukewarm, and argued about whether the clouds to the west meant rain. They did. The walk back took twice as long, and we arrived at the car soaked, laughing, and already planning the next trip.
"@
$voices = @"
And now the weather for the weekend. Expect scattered showers across the region on Saturday, clearing by the evening, with highs around fourteen degrees. Sunday looks brighter, with light winds from the south and plenty of sunshine in the afternoon. In sports, the home side held on for a narrow win last night, despite a late rally from the visitors. Coming up after the break, we will talk to a local baker about the secret to a perfect sourdough loaf, and take a look at the new exhibition opening at the city museum this week.
"@
$s.SelectVoice($names[0])
$s.SetOutputToWaveFile((Join-Path $PSScriptRoot "speech.wav"), $fmt)
$s.Speak($speech)
$other = $names[0]
if ($names.Count -gt 1) { $other = $names[1] }
$s.SelectVoice($other)
$s.SetOutputToWaveFile((Join-Path $PSScriptRoot "voices.wav"), $fmt)
$s.Speak($voices + " " + $voices)
$s.SetOutputToNull()
$s.Dispose()
EOF
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$(wslpath -w "$work/tts.ps1")"
cp "$work/speech.wav" "$work/voices.wav" "$out/"
