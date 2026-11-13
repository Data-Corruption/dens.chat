# The Relay spike on Windows, question 3: captures this machine's sound
# for four minutes from the desktop session, and says when to do what
# takes hands. Something must be playing throughout. The probe mutes the
# output and sets its volume to 0 by itself in the first 70 seconds, and
# puts both back. The log lands in logs\ beside this script.
#
#   powershell -ExecutionPolicy Bypass -File .\capture.ps1
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$logs = Join-Path $here 'logs'
New-Item -ItemType Directory -Force -Path $logs | Out-Null
$log = Join-Path $logs "win-capture-$(Get-Date -Format 'HHmmss').log"
Write-Host 'Capturing for 4 minutes. Keep something playing. The sound mutes and drops to 0 by itself in the first 70 seconds.'
$probe = Start-Process -FilePath (Join-Path $here 'relayprobe.exe') -NoNewWindow -PassThru `
    -ArgumentList '-quiet', '-send', 'system', '-exercise', '-page', 'off', '-for', '240s', '-log', "`"$log`""
$steps = @(
    @(80, 'NOW make another output the default (the speaker icon in the taskbar, or Settings > System > Sound).'),
    @(105, 'Make the first output the default again.'),
    @(130, 'NOW take the output away: unplug it or switch it off.'),
    @(155, 'Plug it back in, or switch it on.'),
    @(185, 'NOW pause what is playing, and leave it paused to the end.')
)
$start = Get-Date
foreach ($step in $steps) {
    $wait = $step[0] - ((Get-Date) - $start).TotalSeconds
    if ($wait -gt 0) { Start-Sleep -Milliseconds ([int]($wait * 1000)) }
    Write-Host ''
    Write-Host ">>> $($step[1])"
}
$probe.WaitForExit()
Write-Host ''
Write-Host "Done. The log: logs\$(Split-Path -Leaf $log)"
