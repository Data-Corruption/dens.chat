# The Relay spike on Windows, as a service: the probe where Dens's service
# runs, in session 0 under an account of its own with a restricted token,
# linking to the probe at -Peer as side a. It makes the service RelayProbe
# from a copy of the probe in C:\ProgramData\RelayProbe, runs it for
# -Seconds, and removes both. -WithRule adds the inbound rule Dens's
# installer would, for the probe's program and port, and removes it after.
#
# From an elevated PowerShell:
#   powershell -ExecutionPolicy Bypass -File .\service.ps1 -Peer 10.0.0.50
#   powershell -ExecutionPolicy Bypass -File .\service.ps1 -Peer 10.0.0.50 -WithRule
param(
    [Parameter(Mandatory = $true)][string]$Peer,
    [string]$Role = 'a',
    [int]$Seconds = 120,
    [switch]$WithRule
)
$ErrorActionPreference = 'Stop'
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { Write-Host 'Run this from an elevated PowerShell.'; exit 1 }

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
if ($Peer -notmatch '^\d+\.\d+\.\d+\.\d+$') { Write-Host "-Peer takes the other machine's address, as 10.0.0.57: the guest prints its own when its run starts. Got: $Peer"; exit 2 }
$logs = Join-Path $here 'logs'
New-Item -ItemType Directory -Force -Path $logs | Out-Null
$name = 'RelayProbe'
$dir = 'C:\ProgramData\RelayProbe'
$exe = Join-Path $dir 'relayprobe-svc.exe'
$svcLog = Join-Path $dir 'service.log'
$kind = 'service'
if ($WithRule) { $kind = 'service-rule' }
$stamp = Get-Date -Format 'HHmmss'
$log = Join-Path $logs "win-$kind-$stamp.log"
$fw = Join-Path $logs "win-$kind-$stamp-firewall.txt"
$rule = 'Relay probe spike UDP'

function Remove-All {
    $ErrorActionPreference = 'Continue'
    & sc.exe stop $name 2>&1 | Out-Null
    Start-Sleep -Seconds 2
    & sc.exe delete $name 2>&1 | Out-Null
    & netsh advfirewall firewall delete rule name="$rule" 2>&1 | Out-Null
    if (Test-Path $dir) { Remove-Item -Recurse -Force $dir }
}

Remove-All
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Copy-Item (Join-Path $here 'relayprobe.exe') $exe
# No path here holds a space, so the command line needs no quotes inside it.
$bin = "$exe -service $name -quiet -peer $Peer -role $Role -send tone -keys synth -page off -for $($Seconds)s -log $svcLog"
& sc.exe create $name binPath= $bin obj= "NT SERVICE\$name" start= demand | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Host "sc create failed: $LASTEXITCODE"; Remove-All; exit 1 }
& sc.exe sidtype $name restricted | Out-Null
& sc.exe privs $name SeChangeNotifyPrivilege | Out-Null
# A restricted token writes only where its own SID is named.
& icacls.exe $dir /grant "NT SERVICE\${name}:(OI)(CI)M" | Out-Null
if ($WithRule) {
    & netsh advfirewall firewall add rule name="$rule" dir=in action=allow enable=yes program="$exe" protocol=UDP localport=47800 | Out-Null
}

"== before, $(Get-Date -Format 'HH:mm:ss'), with rule: $WithRule" | Set-Content $fw
Get-NetFirewallProfile | Format-Table Name, Enabled, DefaultInboundAction, NotifyOnListen -AutoSize | Out-String | Add-Content $fw
Get-NetConnectionProfile | Format-Table InterfaceAlias, NetworkCategory -AutoSize | Out-String | Add-Content $fw

Write-Host "The service runs for $Seconds seconds, linking to $Peer."
& sc.exe start $name | Out-Null
if ($LASTEXITCODE -ne 0) { Write-Host "sc start failed: $LASTEXITCODE" }
$end = (Get-Date).AddSeconds($Seconds + 8)
$shown = 0
while ((Get-Date) -lt $end) {
    Start-Sleep -Seconds 2
    if (Test-Path $svcLog) {
        try {
            $lines = @(Get-Content $svcLog -ErrorAction Stop | Where-Object { $_ -match ' link | peer |where=' })
            if ($lines.Count -gt $shown) {
                $lines[$shown..($lines.Count - 1)] | ForEach-Object { Write-Host $_ }
                $shown = $lines.Count
            }
        } catch { }
    }
    $state = (& sc.exe query $name) -join ' '
    if ($state -match 'STOPPED') { break }
}

"== after, $(Get-Date -Format 'HH:mm:ss')" | Add-Content $fw
$rules = @(Get-NetFirewallApplicationFilter -Program $exe -ErrorAction SilentlyContinue | Get-NetFirewallRule)
if ($rules.Count -eq 0) { 'no rules name the probe' | Add-Content $fw }
else { $rules | Format-Table DisplayName, Direction, Action, Enabled, Profile -AutoSize | Out-String | Add-Content $fw }
if (Test-Path $svcLog) { Copy-Item $svcLog $log } else { Write-Host 'The service left no log.' }
Remove-All
Write-Host "Done. The log: logs\$(Split-Path -Leaf $log)"
