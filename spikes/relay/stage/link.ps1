# The Relay spike on Windows, from the desktop session: links to the probe
# at -Peer as side a, sending this machine's sound and the keys F8 and F9,
# and notes what Defender Firewall holds for the probe before and after.
# The log lands in logs\ beside this script.
#
#   powershell -ExecutionPolicy Bypass -File .\link.ps1 -Peer 10.0.0.50
#   powershell -ExecutionPolicy Bypass -File .\link.ps1 -Peer 10.0.0.50 -For 1h -Page
#
# -Page serves the player and the click page at http://127.0.0.1:47880/.
param(
    [Parameter(Mandatory = $true)][string]$Peer,
    [string]$Role = 'a',
    [string]$For = '120s',
    [string]$Send = 'system',
    [string]$Keys = 'F8,F9',
    [string]$Name = 'link',
    [switch]$Page
)
$ErrorActionPreference = 'Continue'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if ($Peer -notmatch '^\d+\.\d+\.\d+\.\d+$') { Write-Host "-Peer takes the other machine's address, as 10.0.0.57: the guest prints its own when its run starts. Got: $Peer"; exit 2 }
$exe = Join-Path $here 'relayprobe.exe'
$logs = Join-Path $here 'logs'
New-Item -ItemType Directory -Force -Path $logs | Out-Null
$stamp = Get-Date -Format 'HHmmss'
$log = Join-Path $logs "win-$Name-$stamp.log"
$fw = Join-Path $logs "win-$Name-$stamp-firewall.txt"

function Note-Firewall([string]$when) {
    "== $when, $(Get-Date -Format 'HH:mm:ss')" | Add-Content $fw
    try {
        Get-NetFirewallProfile | Format-Table Name, Enabled, DefaultInboundAction, NotifyOnListen -AutoSize | Out-String | Add-Content $fw
        Get-NetConnectionProfile | Format-Table InterfaceAlias, NetworkCategory, IPv4Connectivity -AutoSize | Out-String | Add-Content $fw
        $rules = @(Get-NetFirewallApplicationFilter -Program $exe -ErrorAction SilentlyContinue | Get-NetFirewallRule)
        if ($rules.Count -eq 0) { 'no rules name the probe' | Add-Content $fw }
        else { $rules | Format-Table DisplayName, Direction, Action, Enabled, Profile -AutoSize | Out-String | Add-Content $fw }
    } catch {
        "couldn't read the firewall: $_" | Add-Content $fw
    }
}

Note-Firewall 'before'
$pageArg = 'off'
if ($Page) { $pageArg = '127.0.0.1:47880' }
Write-Host "Linking to $Peer for $For. If Windows asks about the firewall, note what it says and click Allow."
& $exe -peer $Peer -role $Role -send $Send -keys $Keys -page $pageArg -for $For -log $log |
    Select-String -Pattern ' link | peer | keys  got| page  at' | ForEach-Object { $_.Line }
Note-Firewall 'after'
Write-Host "Done. The log: logs\$(Split-Path -Leaf $log)"
