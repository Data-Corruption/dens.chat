#Requires -Version 5.1
<#
Den e2e on Windows: an instance joins a den through Caddy and stays
connected across a den restart, with both ends on one machine.

Instance main hosts the den. Caddy runs as a Windows service (it supports
the Service Control Manager natively) and serves https://den.test with its
internal certificate authority, which the harness trusts in the machine
store; den.test points at loopback in the hosts file. Instance second joins
with an invite from the owner, then main's service restarts, and second
must reconnect.

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

    Step "restart the den's service"
    Invoke-Native -FilePath $Dens -Arguments @("service", "restart") | Out-Null
    $since = Wait-Connected $member $since
    Write-Host "The member reconnected after the restart."
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
