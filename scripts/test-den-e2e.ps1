#Requires -Version 5.1
<#
Den e2e on Windows: an instance joins a den through Caddy, chats, sends a
photo, and stays connected across a den restart; the two start a private
DM; the member recovers their account on a fresh instance and signs the old
one out; then a ban shuts them out. Every end runs on one machine.

Instance main hosts the den. Caddy runs as a Windows service (it supports
the Service Control Manager natively) and serves https://den.test with its
internal certificate authority, which the harness trusts in the machine
store; den.test points at loopback in the hosts file. Instance second joins
with an invite from the owner, the two chat, then main's service restarts;
second must reconnect and see the whole history. The member sends a phone
photo with GPS data, which must reach the owner without it. The owner opens
a DM, which takes no message until both members type each other's check
digits, and then carries text and a photo the den stores only sealed. The
owner shares a checklist with the member, and the two tick different boxes
at the same moment, all of which must stay. Instance fresh stands in for
the member's new machine: it signs in by the den's address with a recovery
code, whose new password must sign second out at once, and reads the DM
only once the member types their DM seal. The new password alone doesn't
sign second in again: fresh approves it, the two comparing digits, and
hands it the seal, with which it reads the DM. Signed out once more from
fresh's device list, second signs in again the same way, with the digits
typed into fresh first, which must go on showing its own. The owner then
bans the member, whose connections must close at once and who can't join
again under the same name. The photo is scripts\test\gps-photo.jpg, next to
this script.

It installs real services and changes the machine's certificate store and
hosts file, and undoes all of it at the end. It refuses to run where Dens
is installed. From an elevated PowerShell:
  bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
  bash scripts/vendor.sh caddy-windows
  powershell -ExecutionPolicy Bypass -File scripts\test-den-e2e.ps1 -ReleaseDir out\windows-e2e -CaddyZip tools\caddy_2.11.4_windows_amd64.zip

-ClientPort, -SecondClientPort and -FreshClientPort move the instances'
client ports off 8484, 18484 and 18486 where those are taken.
-KeepOnFailure leaves a failed setup in place for inspection.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ReleaseDir,
    [Parameter(Mandatory = $true)][string]$CaddyZip,
    [int]$ClientPort = 8484,
    [int]$SecondClientPort = 18484,
    [int]$FreshClientPort = 18486,
    [int]$HttpsPort = 18443,
    [int]$HttpPort = 18080,
    [switch]$KeepOnFailure
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
Add-Type -AssemblyName System.Net.Http

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

# Wait-Out waits until the instance's only den has closed it out, as
# revoked or removed, and returns the reason it gives and how long it took.
function Wait-Out($Browser, [string]$State, [datetime]$Started) {
    while (((Get-Date) - $Started).TotalSeconds -lt 10) {
        $den = @((Invoke-Api $Browser GET "/api/dens").dens)[0]
        if ($den.state -eq $State) {
            return [pscustomobject]@{ Reason = $den.error; Took = [int]((Get-Date) - $Started).TotalMilliseconds }
        }
        Start-Sleep -Milliseconds 100
    }
    Write-Host ((Invoke-Api $Browser GET "/api/dens") | ConvertTo-Json -Depth 5)
    Fail "the den never closed $($Browser.Origin) out as $State"
}

# Invoke-Refused makes a request that must fail, and returns why.
function Invoke-Refused($Browser, [string]$Path, $Body) {
    try {
        Invoke-WebRequest -Uri "$($Browser.Origin)$Path" -Method POST -WebSession $Browser.Session -UseBasicParsing `
            -Headers @{ Origin = $Browser.Origin } -ContentType "application/json" -Body ($Body | ConvertTo-Json -Compress) `
            -TimeoutSec 60 | Out-Null
    } catch {
        return "$($_.ErrorDetails.Message)"
    }
    Fail "POST $Path was accepted"
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

# Send-Tick ticks task N of a message, whose text is "item N", and returns
# the request still in flight, so ticks from two instances can land at once.
function Send-Tick($Browser, [string]$DenID, [string]$Message, [int]$N) {
    $handler = New-Object System.Net.Http.HttpClientHandler
    $handler.CookieContainer = $Browser.Session.Cookies
    $client = New-Object System.Net.Http.HttpClient($handler)
    $client.DefaultRequestHeaders.Add("Origin", $Browser.Origin)
    $body = @{ checked = $true; text = "item $N" } | ConvertTo-Json -Compress
    $content = New-Object System.Net.Http.StringContent($body, [Text.Encoding]::UTF8, "application/json")
    return $client.PostAsync("$($Browser.Origin)/api/dens/$DenID/messages/$Message/tasks/$N", $content)
}

# Read-Shared returns a file's bytes while the service holds it open for
# writing, as it does its database. File.ReadAllBytes shares the file only
# with readers, which Windows refuses then.
function Read-Shared([string]$Path) {
    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite -bor [IO.FileShare]::Delete)
    try {
        $buffer = New-Object IO.MemoryStream
        $stream.CopyTo($buffer)
        return , $buffer.ToArray()
    } finally {
        $stream.Dispose()
    }
}

# Wait-Value polls $Get until it returns something, and returns that.
function Wait-Value([scriptblock]$Get, [string]$What) {
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-Date) -lt $deadline) {
        $value = & $Get
        if ($value) { return $value }
        Start-Sleep -Milliseconds 250
    }
    Fail "timed out waiting for $What"
}

# Get-DMHalf returns the digits a member reads out for a DM, once both
# Dens moved its exchange on. Until then the check leaves half out, and
# strict mode refuses a property that isn't there.
function Get-DMHalf($Browser, [string]$DenID, [string]$DM) {
    return Wait-Value {
        $check = Invoke-Api $Browser GET "/api/dens/$DenID/dms/$DM/check"
        if ($check.PSObject.Properties["half"]) { $check.half }
    } "the DM's digits"
}

# Get-Requests returns the sign-ins an instance shows for a den: those
# waiting for its approval, and those it approved. The den's status leaves
# requests out when there are none.
function Get-Requests($Browser, [string]$DenID) {
    $den = @((Invoke-Api $Browser GET "/api/dens").dens) | Where-Object { $_.den_id -eq $DenID }
    if ($den -and $den.PSObject.Properties["requests"]) { return @($den.requests) }
}

# Approve-SignIn signs one of the member's instances in with the password,
# approved from another: each shows digits, which the harness types into
# the other, as the member would; first into the old instance with
# -OldFirst, which must still show its own digits once it approved.
function Approve-SignIn($New, $Old, [string]$DenID, [string]$Password, [switch]$OldFirst) {
    $signIn = Invoke-Api $New POST "/api/dens/signin" @{ den = $DenURL; username = "bob"; password = $Password }
    if (-not $signIn.PSObject.Properties["pending"] -or $signIn.pending.stage -ne "waiting") { Fail "the password alone signed the device in" }
    $id = $signIn.pending.id
    $request = Wait-Value {
        $waiting = @(Get-Requests $Old $DenID | Where-Object { -not $_.PSObject.Properties["approved"] })
        if ($waiting.Count -gt 0) { $waiting[0].id }
    } "a sign-in asking for approval"
    Invoke-Api $Old POST "/api/dens/$DenID/requests/$request/answer" | Out-Null
    $oldHalf = Wait-Value {
        $r = @(Get-Requests $Old $DenID | Where-Object { $_.id -eq $request })
        if ($r.Count -gt 0 -and $r[0].PSObject.Properties["half"]) { $r[0].half }
    } "the approving instance's digits"
    $newHalf = Wait-Value {
        $p = @(@((Invoke-Api $New GET "/api/dens").sign_ins) | Where-Object { $_.id -eq $id })
        if ($p.Count -gt 0 -and $p[0].PSObject.Properties["half"]) { $p[0].half }
    } "the new instance's digits"
    if ($newHalf -eq $oldHalf) { Fail "both instances show the same digits" }
    if ($OldFirst) {
        Invoke-Api $Old POST "/api/dens/$DenID/requests/$request/approve" @{ digits = $newHalf } | Out-Null
        $shown = Wait-Value {
            $r = @(Get-Requests $Old $DenID | Where-Object { $_.id -eq $request -and $_.PSObject.Properties["approved"] })
            if ($r.Count -gt 0 -and $r[0].PSObject.Properties["half"]) { $r[0].half }
        } "the approved sign-in's digits"
        if ($shown -ne $oldHalf) { Fail "once it approved, the old instance shows $shown" }
        Invoke-Api $New POST "/api/dens/signin/$id/check" @{ digits = $shown } | Out-Null
    } else {
        Invoke-Api $New POST "/api/dens/signin/$id/check" @{ digits = $oldHalf } | Out-Null
        Invoke-Api $Old POST "/api/dens/$DenID/requests/$request/approve" @{ digits = $newHalf } | Out-Null
    }
    $stage = Wait-Value {
        $p = @(@((Invoke-Api $New GET "/api/dens").sign_ins) | Where-Object { $_.id -eq $id })
        if ($p.Count -gt 0 -and $p[0].stage -notin @("waiting", "check", "approved")) { $p[0].stage }
    } "the sign-in to finish"
    if ($stage -ne "done") { Fail "the sign-in ended as $stage" }
    Invoke-Api $New DELETE "/api/dens/signin/$id" | Out-Null
    Invoke-Api $Old DELETE "/api/dens/$DenID/requests/$request" | Out-Null
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
    foreach ($instance in @("main", "second", "fresh")) {
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
        foreach ($instance in @("fresh", "second", "main")) {
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

    Step "a private DM, once both members compare check codes"
    $state = Invoke-Api $owner GET "/api/dens/$denID/state"
    $bob = (@($state.members) | Where-Object { $_.username -eq "bob" }).id
    $dm = (Invoke-Api $owner POST "/api/dens/$denID/dms" @{ member_id = $bob }).id
    Wait-Value { @((Invoke-Api $member GET "/api/dens/$denID/state").channels) | Where-Object { $_.id -eq $dm } } "the DM on the member's side" | Out-Null
    $refused = Invoke-Refused $owner "/api/dens/$denID/channels/$dm/messages" @{ nonce = New-Nonce; text = "too soon" }
    if ($refused -notmatch "compare check codes") { Fail "sending before the check: $refused" }
    Invoke-Api $owner POST "/api/dens/$denID/dms/$dm/key" @{ restart = $false } | Out-Null
    $ownerHalf = Get-DMHalf $owner $denID $dm
    $memberHalf = Get-DMHalf $member $denID $dm
    if ($ownerHalf -eq $memberHalf) { Fail "both members see the same digits" }
    $refused = Invoke-Refused $owner "/api/dens/$denID/dms/$dm/check" @{ digits = "0000 0000 0000 0000" }
    if ($refused -notmatch "don't match") { Fail "the wrong digits: $refused" }
    Invoke-Api $owner POST "/api/dens/$denID/dms/$dm/check" @{ digits = $memberHalf } | Out-Null
    Invoke-Api $member POST "/api/dens/$denID/dms/$dm/check" @{ digits = $ownerHalf } | Out-Null
    Send-Message $owner $denID $dm "a private word"
    $history = Get-History $member $denID $dm
    if ($history -ne "a private word") { Fail "the member's DM reads: $history" }
    try {
        $upload = Invoke-WebRequest -Uri "$($member.Origin)/api/dens/$denID/uploads?channel=$dm" -Method POST -WebSession $member.Session `
            -UseBasicParsing -Headers @{ Origin = $member.Origin; "Dens-Filename" = "IMG_0002.jpg" } `
            -ContentType "application/octet-stream" -InFile $photo -TimeoutSec 60
    } catch {
        Fail "the DM upload failed: $($_.ErrorDetails.Message) $($_.Exception.Message)"
    }
    $file = $upload.Content | ConvertFrom-Json
    if (-not $file.stripped -or $file.width -ne 300 -or $file.height -ne 400 -or -not $file.thumb) {
        Fail "the DM upload came back as $($upload.Content)"
    }
    Invoke-Api $member POST "/api/dens/$denID/channels/$dm/messages" @{ nonce = New-Nonce; text = ""; attachments = @($file.id) } | Out-Null
    foreach ($variant in @("", "/thumb")) {
        $got = Join-Path $Work "dm-photo.jpg"
        $resp = Invoke-WebRequest -Uri "$($owner.Origin)/api/dens/$denID/files/$($file.id)$variant" -WebSession $owner.Session `
            -UseBasicParsing -OutFile $got -PassThru -TimeoutSec 60
        if ("$($resp.Headers['Content-Type'])" -ne "image/jpeg") { Fail "the DM photo$variant came as $($resp.Headers['Content-Type'])" }
        $text = $latin1.GetString([IO.File]::ReadAllBytes($got))
        foreach ($marker in @("TestPhone", "GPSLatitude", "Taken at home", "ns.adobe.com")) {
            if ($text.Contains($marker)) { Fail "the DM photo$variant arrived with '$marker'" }
        }
    }
    # The -shm file holds only the write-ahead log's index, where a write in
    # progress locks bytes that would refuse the read.
    $dbFiles = @(Get-ChildItem -File (Join-Path $DataRoot "main\data\db") | Where-Object { $_.Name -notlike "*-shm" })
    if ($dbFiles.Count -eq 0) { Fail "no database files to look in" }
    foreach ($f in $dbFiles) {
        if ($latin1.GetString((Read-Shared $f.FullName)).Contains("a private word")) { Fail "$($f.Name) holds the DM's text" }
    }
    Write-Host "The DM took messages only after both typed each other's digits; its text and photo reached the other side sealed."

    Step "two members tick one checklist at the same moment"
    $text = (0..5 | ForEach-Object { "[ ] item $_" }) -join "`n"
    $list = Invoke-Api $owner POST "/api/dens/$denID/channels/$channel/messages" @{ nonce = New-Nonce; text = $text; editors = @($bob) }
    $ticks = foreach ($n in 0..5) {
        $who = $owner
        if ($n % 2 -eq 1) { $who = $member }
        Send-Tick $who $denID $list.id $n
    }
    [System.Threading.Tasks.Task]::WaitAll([System.Threading.Tasks.Task[]]$ticks)
    foreach ($t in $ticks) {
        if (-not $t.Result.IsSuccessStatusCode) { Fail "a tick failed with $([int]$t.Result.StatusCode)" }
    }
    $page = Invoke-Api $member GET "/api/dens/$denID/channels/$channel/messages?limit=10"
    $got = (@($page.messages) | Where-Object { $_.id -eq $list.id }).text
    $want = (0..5 | ForEach-Object { "[x] item $_" }) -join "`n"
    if ($got -ne $want) { Fail "the checklist reads: $got" }
    Write-Host "Every tick from both members stayed."

    Step "recover the member's account on instance fresh"
    Invoke-Installer (@("-Instance", "fresh", "-ClientPort", $FreshClientPort) + $userArgs)
    $fresh = New-Browser "fresh" $FreshClientPort
    $started = Get-Date
    # By address, as someone with no invite would, and in the wrong case.
    $recovered = Invoke-Api $fresh POST "/api/dens/signin" @{
        den = $DenURL; username = "Bob"; recovery_code = @($joined.recovery_codes)[0]; password = "new bob password"
    }
    if ($recovered.recovery_codes_left -ne 9 -or $recovered.signed_out -ne 1) {
        Fail "recovering left $($recovered.recovery_codes_left) codes and signed out $($recovered.signed_out) devices"
    }
    $ownerDen = @((Invoke-Api $owner GET "/api/dens").dens)[0]
    if ($recovered.den.fingerprint -ne $ownerDen.fingerprint) { Fail "instance fresh found den $($recovered.den.fingerprint), not the owner's" }
    $out = Wait-Out $member "revoked" $started
    if (-not $out.Reason.StartsWith("Your den password was changed on another device")) { Fail "instance second was told: '$($out.Reason)'" }
    if ($out.Took -gt 5000) { Fail "the new password took $($out.Took) ms to sign instance second out" }
    Wait-Connected $fresh 0 | Out-Null
    Write-Host "Instance fresh signed in with a recovery code, shows the owner's den ID, and signed second out within $($out.Took) ms."
    if (@((Invoke-Api $fresh GET "/api/dens").dens)[0].seal) { Fail "a recovery code brought a DM seal" }
    $history = Get-History $fresh $denID $dm
    if ($history -match "a private word") { Fail "instance fresh read the DM without the seal" }
    if (-not $joined.seal_new) { Fail "joining showed no new DM seal" }
    Invoke-Api $fresh POST "/api/dens/$denID/seal" @{ seal = $joined.seal } | Out-Null
    $history = Get-History $fresh $denID $dm
    if (-not $history.StartsWith("a private word")) { Fail "with the seal typed, the DM reads: $history" }
    Write-Host "Instance fresh reads the DM once the member typed their seal."
    $refused = Invoke-Refused $member "/api/dens/signin" @{ den = $DenURL; username = "bob"; password = "bob password" }
    if ($refused -notmatch "don't match") { Fail "signing in with the old password: $refused" }
    Approve-SignIn $member $fresh $denID "new bob password"
    Wait-Connected $member 0 | Out-Null
    $history = Get-History $member $denID $dm
    if (-not $history.StartsWith("a private word")) { Fail "the approved instance's DM reads: $history" }
    Write-Host "The old password is gone; the new one signs instance second in again once fresh approves it, and hands it the seal."

    Step "sign instance second out from instance fresh"
    $devices = @((Invoke-Api $fresh GET "/api/dens/$denID/devices").devices)
    if ($devices.Count -ne 2) { Fail "the member has $($devices.Count) devices" }
    $old = @($devices | Where-Object { -not $_.PSObject.Properties["current"] -or -not $_.current })[0]
    $started = Get-Date
    Invoke-Api $fresh DELETE "/api/dens/$denID/devices/$($old.key_id)" | Out-Null
    $out = Wait-Out $member "revoked" $started
    if ($out.Reason -ne "This device was signed out of this den. Sign in again with your den password.") {
        Fail "instance second was told: '$($out.Reason)'"
    }
    if ($out.Took -gt 5000) { Fail "signing out took $($out.Took) ms to reach instance second" }
    Approve-SignIn $member $fresh $denID "new bob password" -OldFirst
    Wait-Connected $member 0 | Out-Null
    Write-Host "Instance second was signed out within $($out.Took) ms, and signed in again with fresh's approval, typed there first."

    Step "ban the member"
    $started = Get-Date
    Invoke-Api $owner POST "/api/dens/$denID/members/$bob/remove" @{ ban = $true } | Out-Null
    $out = Wait-Out $member "removed" $started
    if ($out.Reason -ne "You were banned from this den.") { Fail "after $($out.Took) ms the member was told: '$($out.Reason)'" }
    if ($out.Took -gt 5000) { Fail "the ban took $($out.Took) ms to reach the member" }
    $freshOut = Wait-Out $fresh "removed" $started
    if ($freshOut.Reason -ne "You were banned from this den.") { Fail "instance fresh was told: '$($freshOut.Reason)'" }
    Write-Host "The ban closed the member's connections within $($out.Took) ms."
    $invite = (Invoke-Api $owner POST "/api/dens/$denID/invites" @{ expires_in = 3600; max_uses = 1 }).invite
    $refused = Invoke-Refused $member "/api/dens/join" @{ invite = $invite; username = "bob"; display_name = "Bob"; password = "bob password" }
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
