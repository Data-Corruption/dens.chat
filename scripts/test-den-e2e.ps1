#Requires -Version 5.1
<#
Den e2e on Windows: an instance joins a den through Caddy, chats, sends a
photo, and stays connected across a den restart; then a ban shuts it out.
Both ends run on one machine.

Instance main hosts the den. Caddy runs as a Windows service (it supports
the Service Control Manager natively) and serves https://den.test with its
internal certificate authority, which the harness trusts in the machine
store; den.test points at loopback in the hosts file. Instance second joins
with an invite from the owner, the two chat, then main's service restarts;
second must reconnect and see the whole history. The owner then sends a
DM, the member sends a phone photo with GPS data, which must reach the
owner without it, and the owner bans second's member, whose connection must
close at once and who can't join again under the same name. The photo is
scripts\test\gps-photo.jpg, next to this script.

It installs real services and changes the machine's certificate store and
hosts file, and undoes all of it at the end. It refuses to run where Dens
is installed. From an elevated PowerShell:
  bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
  bash scripts/vendor.sh caddy-windows
  powershell -ExecutionPolicy Bypass -File scripts\test-den-e2e.ps1 -ReleaseDir out\windows-e2e -CaddyZip tools\caddy_2.11.4_windows_amd64.zip

-ClientPort and -SecondClientPort move the instances' client ports off 8484
and 18484 where those are taken. -KeepOnFailure leaves a failed setup in
place for inspection.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ReleaseDir,
    [Parameter(Mandatory = $true)][string]$CaddyZip,
    [int]$ClientPort = 8484,
    [int]$SecondClientPort = 18484,
    [int]$HttpsPort = 18443,
    [int]$HttpPort = 18080,
    [switch]$KeepOnFailure
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$BinaryDir = Join-Path $env:ProgramFiles "Dens"
$Dens = Join-Path $BinaryDir "dens.exe"
$DataRoot = Join-Path $env:ProgramData "Dens"
$RunId = [Guid]::NewGuid().ToString("N").Substring(0, 8)
$Work = Join-Path $env:ProgramData "dens-den-e2e-$RunId"
$CaddyService = "dens-e2e-caddy"
$HostsFile = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
$DenURL = "https://den.test:$HttpsPort"
$script:CurrentStep = "setup"
$script:HostsBackup = $null
$script:CertThumbprint = $null

function Step([string]$Name) {
    $script:CurrentStep = $Name
    Write-Host ""
    Write-Host "== $Name"
}

function Fail([string]$Message) {
    throw "FAIL ($script:CurrentStep): $Message"
}

function Invoke-Native {
    param([string]$FilePath, [string[]]$Arguments, [switch]$AllowFailure)
    $saved = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        $output = & $FilePath @Arguments 2>&1
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $saved
    }
    $text = (@($output) | ForEach-Object { "$_" }) -join "`n"
    if ($text) { Write-Host $text }
    if ($code -ne 0 -and -not $AllowFailure) {
        Fail "$(Split-Path -Leaf $FilePath) $($Arguments -join ' ') exited with $code"
    }
    return [pscustomobject]@{ Code = $code; Output = $text }
}

function Invoke-Installer {
    param([string[]]$Arguments)
    $env:APP_RELEASE_URL = ([Uri]($Release.TrimEnd("\") + "\")).AbsoluteUri
    try {
        Invoke-Native -FilePath "powershell.exe" -Arguments (@("-NoProfile", "-NonInteractive",
                "-ExecutionPolicy", "Bypass", "-File", (Join-Path $Release "install.ps1")) + $Arguments) | Out-Null
    } finally {
        Remove-Item Env:APP_RELEASE_URL -ErrorAction SilentlyContinue
    }
}

# A paired browser for one instance: its session and origin.
function New-Browser([string]$Instance, [int]$Port) {
    $origin = "http://127.0.0.1:$Port"
    $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $url = (Invoke-Native -FilePath $Dens -Arguments @("open", "--print", "--instance", $Instance)).Output.Trim()
    $browser = [pscustomobject]@{ Origin = $origin; Session = $session }
    Invoke-Api $browser POST "/api/pair" @{ token = ($url -split "#token=")[-1] } | Out-Null
    Invoke-Api $browser POST "/api/password" @{ password = "local password" } | Out-Null
    return $browser
}

function Invoke-Api {
    param($Browser, [string]$Method, [string]$Path, $Body = $null)
    $request = @{
        Uri             = "$($Browser.Origin)$Path"
        Method          = $Method
        WebSession      = $Browser.Session
        UseBasicParsing = $true
        Headers         = @{ Origin = $Browser.Origin }
        TimeoutSec      = 60
    }
    if ($null -ne $Body) {
        $request.Body = ($Body | ConvertTo-Json -Compress)
        $request.ContentType = "application/json"
    }
    try {
        $resp = Invoke-WebRequest @request
    } catch {
        $detail = $_.Exception.Message
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $detail = $_.ErrorDetails.Message }
        Fail "$Method $Path failed: $detail"
    }
    if ($resp.Content) { return $resp.Content | ConvertFrom-Json }
    return $null
}

# Wait-Connected waits until the instance's only den is connected with a
# state newer than $After (ms), and returns when it connected.
function Wait-Connected($Browser, [long]$After) {
    $deadline = (Get-Date).AddSeconds(90)
    while ((Get-Date) -lt $deadline) {
        $view = Invoke-Api $Browser GET "/api/dens"
        $dens = @($view.dens)
        if ($dens.Count -eq 1 -and $dens[0].state -eq "connected" -and [long]$dens[0].since -gt $After) {
            return [long]$dens[0].since
        }
        Start-Sleep -Milliseconds 500
    }
    Write-Host ((Invoke-Api $Browser GET "/api/dens") | ConvertTo-Json -Depth 5)
    Fail "the den didn't connect"
}

function New-Nonce {
    $bytes = New-Object byte[] 16
    [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    return [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

function Send-Message($Browser, [string]$DenID, [string]$Channel, [string]$Text) {
    Invoke-Api $Browser POST "/api/dens/$DenID/channels/$Channel/messages" @{ nonce = New-Nonce; text = $Text } | Out-Null
}

function Get-History($Browser, [string]$DenID, [string]$Channel) {
    $page = Invoke-Api $Browser GET "/api/dens/$DenID/channels/$Channel/messages?limit=100"
    return (@($page.messages) | ForEach-Object { $_.text }) -join "|"
}

function Wait-Channel($Browser, [string]$DenID) {
    $deadline = (Get-Date).AddSeconds(15)
    while ((Get-Date) -lt $deadline) {
        $state = Invoke-Api $Browser GET "/api/dens/$DenID/state"
        if (@($state.channels).Count -gt 0) { return @($state.channels)[0].id }
        Start-Sleep -Milliseconds 250
    }
    Fail "no channel reached instance second"
}

function Test-Installed {
    if (Get-Service -Name "dens-*" -ErrorAction SilentlyContinue) { return $true }
    return (Test-Path -LiteralPath $DataRoot) -or (Test-Path -LiteralPath $Dens)
}

function Show-Diagnostics {
    foreach ($instance in @("main", "second")) {
        $log = Join-Path $DataRoot "$instance\data\logs\latest.log"
        if (Test-Path -LiteralPath $log) {
            Write-Host "--- $log"
            Get-Content -LiteralPath $log -Tail 40 | ForEach-Object { Write-Host $_ }
        }
    }
    $caddyLog = Join-Path $Work "caddy\caddy.log"
    if (Test-Path -LiteralPath $caddyLog) {
        Write-Host "--- $caddyLog"
        Get-Content -LiteralPath $caddyLog -Tail 20 | ForEach-Object { Write-Host $_ }
    }
}

function Remove-Setup {
    if (Test-Path -LiteralPath $Dens) {
        foreach ($instance in @("second", "main")) {
            if (Test-Path -LiteralPath (Join-Path $DataRoot $instance)) {
                Invoke-Native -FilePath $Dens -Arguments @("uninstall", "--instance", $instance, "--yes") -AllowFailure | Out-Null
            }
        }
    }
    if (Get-Service -Name $CaddyService -ErrorAction SilentlyContinue) {
        Stop-Service -Name $CaddyService -Force -ErrorAction SilentlyContinue
        Invoke-Native -FilePath "sc.exe" -Arguments @("delete", $CaddyService) -AllowFailure | Out-Null
    }
    if ($script:CertThumbprint) {
        Remove-Item -LiteralPath "Cert:\LocalMachine\Root\$($script:CertThumbprint)" -ErrorAction SilentlyContinue
    }
    if ($null -ne $script:HostsBackup) {
        [IO.File]::WriteAllText($HostsFile, $script:HostsBackup)
    }
    Remove-Item -LiteralPath $Work -Recurse -Force -ErrorAction SilentlyContinue
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run the harness from an elevated PowerShell (Run as administrator)."
}
if (Test-Installed) {
    throw "Dens is installed on this machine. The harness installs and removes the real services; run it where Dens isn't installed."
}
if (Get-Service -Name $CaddyService -ErrorAction SilentlyContinue) {
    throw "A $CaddyService service exists from an earlier run; remove it with: sc.exe delete $CaddyService"
}

$passed = $false
try {
    New-Item -ItemType Directory -Path $Work | Out-Null
    $Release = Join-Path $Work "release"
    Copy-Item -LiteralPath (Resolve-Path -LiteralPath $ReleaseDir).ProviderPath -Destination $Release -Recurse
    $env:APP_SKIP_VERIFY = "true"
    $me = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $console = (Get-CimInstance Win32_ComputerSystem).UserName
    $userArgs = @()
    if (-not $console -or $console -ne $me) { $userArgs = @("-User", $me) }

    Step "install instance main, hosting a den"
    $mainArgs = @("-Den") + $userArgs
    if ($ClientPort -ne 8484) { $mainArgs += @("-ClientPort", $ClientPort) }
    Invoke-Installer $mainArgs
    $owner = New-Browser "main" $ClientPort

    Step "run Caddy as a Windows service"
    $caddyDir = Join-Path $Work "caddy"
    Expand-Archive -LiteralPath (Resolve-Path -LiteralPath $CaddyZip).ProviderPath -DestinationPath $caddyDir
    $caddy = Join-Path $caddyDir "caddy.exe"
    $storage = (Join-Path $caddyDir "data") -replace "\\", "/"
    $caddyLog = (Join-Path $caddyDir "caddy.log") -replace "\\", "/"
    $caddyfile = Join-Path $caddyDir "Caddyfile"
    $config = @"
{
	skip_install_trust
	storage file_system $storage
	http_port $HttpPort
	https_port $HttpsPort
	log {
		output file $caddyLog
	}
}
den.test:$HttpsPort {
	tls internal
	reverse_proxy 127.0.0.1:8485
}
"@
    [IO.File]::WriteAllText($caddyfile, $config)
    New-Service -Name $CaddyService -BinaryPathName "`"$caddy`" run --config `"$caddyfile`" --adapter caddyfile" -StartupType Manual | Out-Null
    Start-Service -Name $CaddyService
    $root = Join-Path $caddyDir "data\pki\authorities\local\root.crt"
    $deadline = (Get-Date).AddSeconds(30)
    while (-not (Test-Path -LiteralPath $root)) {
        if ((Get-Date) -gt $deadline) { Fail "Caddy didn't create its certificate authority" }
        Start-Sleep -Milliseconds 250
    }
    $cert = Import-Certificate -FilePath $root -CertStoreLocation "Cert:\LocalMachine\Root"
    $script:CertThumbprint = $cert.Thumbprint
    $script:HostsBackup = [IO.File]::ReadAllText($HostsFile)
    [IO.File]::AppendAllText($HostsFile, "`r`n127.0.0.1 den.test`r`n")

    Step "create the den and invite a member"
    $created = Invoke-Api $owner POST "/api/den" @{
        name = "E2E Den"; url = $DenURL; username = "alice"; display_name = "Alice"; password = "den password"
    }
    if (@($created.recovery_codes).Count -ne 10) { Fail "the owner didn't get 10 recovery codes" }
    $denID = $created.den.den_id
    Wait-Connected $owner 0 | Out-Null
    $invite = (Invoke-Api $owner POST "/api/dens/$denID/invites" @{ expires_in = 3600; max_uses = 1 }).invite

    Step "install instance second and join through Caddy"
    Invoke-Installer (@("-Instance", "second", "-ClientPort", $SecondClientPort) + $userArgs)
    $member = New-Browser "second" $SecondClientPort
    $preview = Invoke-Api $member POST "/api/dens/preview" @{ invite = $invite }
    if ($preview.den.name -ne "E2E Den") { Fail "the preview named the den $($preview.den.name)" }
    $joined = Invoke-Api $member POST "/api/dens/join" @{
        invite = $invite; username = "bob"; display_name = "Bob"; password = "bob password"
    }
    if ($joined.den.role -ne "member") { Fail "joined as $($joined.den.role)" }
    $since = Wait-Connected $member 0

    Step "chat"
    Invoke-Api $owner POST "/api/dens/$denID/channels" @{ name = "general" } | Out-Null
    $channel = Wait-Channel $member $denID
    Send-Message $member $denID $channel "hello from the member"
    Send-Message $owner $denID $channel "hello back, @bob"
    $history = Get-History $owner $denID $channel
    if ($history -ne "hello from the member|hello back, @bob") { Fail "the owner's history is: $history" }

    Step "restart the den's service"
    Invoke-Native -FilePath $Dens -Arguments @("service", "restart") | Out-Null
    $since = Wait-Connected $member $since
    Write-Host "The member reconnected after the restart."
    Wait-Connected $owner 0 | Out-Null
    Send-Message $owner $denID $channel "after the restart"
    $history = Get-History $member $denID $channel
    if ($history -ne "hello from the member|hello back, @bob|after the restart") { Fail "the member's history after the restart is: $history" }

    Step "a direct message"
    $state = Invoke-Api $owner GET "/api/dens/$denID/state"
    $bob = (@($state.members) | Where-Object { $_.username -eq "bob" }).id
    $dm = (Invoke-Api $owner POST "/api/dens/$denID/dms" @{ member_id = $bob }).id
    Send-Message $owner $denID $dm "a private word"
    $deadline = (Get-Date).AddSeconds(15)
    while ((Get-Date) -lt $deadline) {
        $memberState = Invoke-Api $member GET "/api/dens/$denID/state"
        if (@($memberState.channels) | Where-Object { $_.id -eq $dm }) { break }
        Start-Sleep -Milliseconds 250
    }
    $history = Get-History $member $denID $dm
    if ($history -ne "a private word") { Fail "the member's DM reads: $history" }

    Step "a phone photo with GPS data"
    $photo = Join-Path $PSScriptRoot "test\gps-photo.jpg"
    try {
        $upload = Invoke-WebRequest -Uri "$($member.Origin)/api/dens/$denID/uploads" -Method POST -WebSession $member.Session `
            -UseBasicParsing -Headers @{ Origin = $member.Origin; "Dens-Filename" = "IMG_0001.jpg" } `
            -ContentType "application/octet-stream" -InFile $photo -TimeoutSec 60
    } catch {
        Fail "the upload failed: $($_.ErrorDetails.Message) $($_.Exception.Message)"
    }
    $file = $upload.Content | ConvertFrom-Json
    # Orientation 6 turns the 400x300 photo a quarter: it shows 300x400.
    if (-not $file.stripped -or $file.width -ne 300 -or $file.height -ne 400 -or -not $file.thumb) {
        Fail "the upload came back as $($upload.Content)"
    }
    Invoke-Api $member POST "/api/dens/$denID/channels/$channel/messages" @{ nonce = New-Nonce; text = ""; attachments = @($file.id) } | Out-Null
    $latin1 = [Text.Encoding]::GetEncoding(28591)
    foreach ($variant in @("", "/thumb")) {
        $got = Join-Path $Work "photo.jpg"
        $resp = Invoke-WebRequest -Uri "$($owner.Origin)/api/dens/$denID/files/$($file.id)$variant" -WebSession $owner.Session `
            -UseBasicParsing -OutFile $got -PassThru -TimeoutSec 60
        if ("$($resp.Headers['Content-Type'])" -ne "image/jpeg") { Fail "the photo$variant came as $($resp.Headers['Content-Type'])" }
        $text = $latin1.GetString([IO.File]::ReadAllBytes($got))
        foreach ($marker in @("TestPhone", "GPSLatitude", "Taken at home", "ns.adobe.com")) {
            if ($text.Contains($marker)) { Fail "the photo$variant arrived with '$marker'" }
        }
    }
    $stored = @(Get-ChildItem -File (Join-Path $DataRoot "main\data\uploads"))
    foreach ($f in $stored) {
        if ($latin1.GetString([IO.File]::ReadAllBytes($f.FullName)).Contains("JFIF")) { Fail "$($f.Name) is stored in the clear" }
    }
    Write-Host "The photo arrived stripped, with a preview; the den holds $($stored.Count) sealed files."

    Step "ban the member"
    $started = Get-Date
    Invoke-Api $owner POST "/api/dens/$denID/members/$bob/remove" @{ ban = $true } | Out-Null
    $reason = ""
    while (((Get-Date) - $started).TotalSeconds -lt 10) {
        $den = @((Invoke-Api $member GET "/api/dens").dens)[0]
        if ($den.state -eq "removed") { $reason = $den.error; break }
        Start-Sleep -Milliseconds 100
    }
    $took = [int]((Get-Date) - $started).TotalMilliseconds
    if ($reason -ne "You were banned from this den.") { Fail "after $took ms the member was told: '$reason'" }
    if ($took -gt 5000) { Fail "the ban took $took ms to reach the member" }
    Write-Host "The ban closed the member's connection within $took ms."
    $invite = (Invoke-Api $owner POST "/api/dens/$denID/invites" @{ expires_in = 3600; max_uses = 1 }).invite
    $refused = $null
    try {
        $body = @{ invite = $invite; username = "bob"; display_name = "Bob"; password = "bob password" } | ConvertTo-Json -Compress
        Invoke-WebRequest -Uri "$($member.Origin)/api/dens/join" -Method POST -WebSession $member.Session -UseBasicParsing `
            -Headers @{ Origin = $member.Origin } -ContentType "application/json" -Body $body -TimeoutSec 60 | Out-Null
    } catch {
        $refused = "$($_.ErrorDetails.Message)"
    }
    if ($null -eq $refused) { Fail "joining again after the ban was accepted" }
    if ($refused -notmatch "banned") { Fail "rejoining after the ban: $refused" }
    Write-Host "The banned member can't come back as bob."
    $passed = $true
} catch {
    Write-Host ""
    Write-Host $_.Exception.Message
    Show-Diagnostics
} finally {
    if ($passed -or -not $KeepOnFailure) {
        Remove-Setup
    } else {
        Write-Host ""
        Write-Host "Kept the failed setup (-KeepOnFailure). The hosts file and certificate store still hold den.test's entries."
    }
    Remove-Item Env:APP_SKIP_VERIFY -ErrorAction SilentlyContinue
}

Write-Host ""
if ($passed) {
    Write-Host "Windows den e2e: PASSED"
    exit 0
}
Write-Host "Windows den e2e: FAILED"
exit 1
