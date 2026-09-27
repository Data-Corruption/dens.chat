#Requires -Version 5.1
<#
Windows lifecycle e2e. Installs Dens as a service through install.ps1 from
fixture releases and runs the flows the Linux harness runs: pair a browser,
set the password, back up, refuse another account, restart the service,
update, add a second instance that hosts a den, restore, and uninstall.

It installs the real service and paths, so it refuses to run where Dens is
installed. From an elevated PowerShell:
  bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
  powershell -ExecutionPolicy Bypass -File scripts/test-lifecycle-e2e.ps1 -ReleaseDir out/windows-e2e

-InstallerCandidate installs with an exact rendered install.ps1 instead of
the fixture's. -Backup also restores a backup made elsewhere, such as by the
Linux harness, with the same test password. -SaveBackup keeps the backup
this run makes. A restarted service stands in for a reboot.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ReleaseDir,
    [string]$InstallerCandidate = "",
    [string]$Backup = "",
    [string]$SaveBackup = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$Password = "correct horse battery staple"
$ClientUrl = "http://127.0.0.1:8484"
$DenHealth = "http://127.0.0.1:8485/healthz"
$BinaryDir = Join-Path $env:ProgramFiles "Dens"
$Dens = Join-Path $BinaryDir "dens.exe"
$DataRoot = Join-Path $env:ProgramData "Dens"
$RunId = [Guid]::NewGuid().ToString("N").Substring(0, 8)
# Outside every user profile, so the other-account task can reach it.
$Work = Join-Path $env:ProgramData "dens-e2e-$RunId"
$TaskName = "dens-e2e-$RunId"
$WerKey = "HKLM:\SOFTWARE\Microsoft\Windows\Windows Error Reporting\ExcludedApplications"
$script:CurrentStep = "setup"
$script:Session = $null

function Step([string]$Name) {
    $script:CurrentStep = $Name
    Write-Host ""
    Write-Host "== $Name"
}

function Fail([string]$Message) {
    throw "FAIL ($script:CurrentStep): $Message"
}

# Invoke-Native runs a program, echoes its output, and returns the output and
# exit code. Windows PowerShell 5.1 turns native stderr into error records,
# so the exit code decides.
function Invoke-Native {
    param([string]$FilePath, [string[]]$Arguments, [string]$Stdin = $null, [switch]$AllowFailure)
    $saved = $ErrorActionPreference
    try {
        $ErrorActionPreference = "Continue"
        if ($null -ne $Stdin) {
            $output = $Stdin | & $FilePath @Arguments 2>&1
        } else {
            $output = & $FilePath @Arguments 2>&1
        }
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

function Invoke-Dens {
    param([string[]]$Arguments, [string]$Stdin = $null, [switch]$AllowFailure)
    return Invoke-Native -FilePath $Dens -Arguments $Arguments -Stdin $Stdin -AllowFailure:$AllowFailure
}

function Invoke-Installer {
    param([string]$Root, [string[]]$Arguments)
    $env:APP_RELEASE_URL = ConvertTo-FileUrl $Root
    try {
        $script = Join-Path $Root "install.ps1"
        Invoke-Native -FilePath "powershell.exe" -Arguments (@("-NoProfile", "-NonInteractive",
                "-ExecutionPolicy", "Bypass", "-File", $script) + $Arguments) | Out-Null
    } finally {
        Remove-Item Env:APP_RELEASE_URL -ErrorAction SilentlyContinue
    }
}

function ConvertTo-FileUrl([string]$Path) {
    return ([Uri]($Path.TrimEnd("\") + "\")).AbsoluteUri
}

# Invoke-Api calls the client listener with the paired browser's cookies.
function Invoke-Api {
    param([string]$Method, [string]$Path, $Body = $null, [switch]$AllowFailure)
    $request = @{
        Uri             = "$ClientUrl$Path"
        Method          = $Method
        WebSession      = $script:Session
        UseBasicParsing = $true
        Headers         = @{ Origin = $ClientUrl }
        TimeoutSec      = 30
    }
    if ($null -ne $Body) {
        $request.Body = ($Body | ConvertTo-Json -Compress)
        $request.ContentType = "application/json"
    }
    try {
        return Invoke-WebRequest @request
    } catch {
        if ($AllowFailure) { return $null }
        Fail "$Method $Path failed: $($_.Exception.Message)"
    }
}

# Get-StatusLine sends a request with an arbitrary Host header, which
# Invoke-WebRequest won't, and returns the response's status line.
function Get-StatusLine([string]$HostHeader) {
    $client = New-Object Net.Sockets.TcpClient("127.0.0.1", 8484)
    try {
        $stream = $client.GetStream()
        $request = [Text.Encoding]::ASCII.GetBytes("GET / HTTP/1.1`r`nHost: $HostHeader`r`nConnection: close`r`n`r`n")
        $stream.Write($request, 0, $request.Length)
        return (New-Object IO.StreamReader($stream)).ReadLine()
    } finally {
        $client.Close()
    }
}

function Assert-State([string]$Instance, [string]$Version) {
    $path = Join-Path $DataRoot "$Instance\control\state.json"
    $state = [IO.File]::ReadAllText($path)
    if ($state -notmatch ('"phase":"ready","version":"' + [regex]::Escape($Version) + '"')) {
        Fail "instance $Instance state is $state, want ready $Version"
    }
}

function Assert-Status([string]$Instance, [string]$Pattern) {
    $status = (Invoke-Dens @("status", "--instance", $Instance)).Output
    if ($status -notmatch $Pattern) { Fail "dens status for $Instance doesn't match '$Pattern'" }
}

function Wait-Status([string]$Instance) {
    $deadline = (Get-Date).AddSeconds(60)
    while ((Invoke-Dens @("status", "--instance", $Instance) -AllowFailure).Code -ne 0) {
        if ((Get-Date) -gt $deadline) { Fail "instance $Instance didn't answer within 60 seconds" }
        Start-Sleep -Milliseconds 500
    }
}

function Assert-Running([string]$Service) {
    $svc = Get-Service -Name $Service -ErrorAction SilentlyContinue
    if ($null -eq $svc -or $svc.Status -ne "Running") { Fail "$Service is not running" }
}

function Test-Installed {
    if (Get-Service -Name "dens-*" -ErrorAction SilentlyContinue) { return $true }
    return (Test-Path -LiteralPath $DataRoot) -or (Test-Path -LiteralPath $Dens)
}

function Test-OnPath {
    $path = [Environment]::GetEnvironmentVariable("Path", "Machine")
    return @($path -split ";" | Where-Object { $_.TrimEnd("\") -ieq $BinaryDir }).Count -gt 0
}

function Test-WerExcluded {
    $key = Get-Item -LiteralPath $WerKey -ErrorAction SilentlyContinue
    return ($null -ne $key) -and ($key.GetValueNames() -contains "dens.exe")
}

function Get-MediaRules {
    return @(Get-NetFirewallRule -ErrorAction SilentlyContinue |
            Where-Object { $_.DisplayName -like "Dens (*) media *" })
}

# Invoke-AsLocalService runs dens status as LOCAL SERVICE through a scheduled
# task: a real second account without creating one.
function Invoke-AsLocalService {
    $dir = Join-Path $Work "other"
    New-Item -ItemType Directory -Path $dir | Out-Null
    $script = Join-Path $dir "status.cmd"
    $lines = @(
        "@echo off",
        "`"$Dens`" status > `"%~dp0out.txt`" 2>&1",
        "echo %ERRORLEVEL% > `"%~dp0code.tmp`"",
        "move /y `"%~dp0code.tmp`" `"%~dp0code.txt`" > nul"
    )
    [IO.File]::WriteAllText($script, ($lines -join "`r`n") + "`r`n")
    Invoke-Native -FilePath "icacls.exe" -Arguments @($dir, "/grant", "*S-1-5-19:(OI)(CI)M") | Out-Null
    $action = New-ScheduledTaskAction -Execute "cmd.exe" -Argument "/c `"$script`""
    $principal = New-ScheduledTaskPrincipal -UserId "NT AUTHORITY\LOCAL SERVICE" -LogonType ServiceAccount
    Register-ScheduledTask -TaskName $TaskName -Action $action -Principal $principal | Out-Null
    try {
        Start-ScheduledTask -TaskName $TaskName
        $done = Join-Path $dir "code.txt"
        $deadline = (Get-Date).AddSeconds(60)
        while (-not (Test-Path -LiteralPath $done)) {
            if ((Get-Date) -gt $deadline) { Fail "the LOCAL SERVICE task didn't finish" }
            Start-Sleep -Milliseconds 250
        }
    } finally {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    }
    return [pscustomobject]@{
        Code   = ([IO.File]::ReadAllText($done)).Trim()
        Output = [IO.File]::ReadAllText((Join-Path $dir "out.txt")).Trim()
    }
}

function Remove-Installation {
    if (-not (Test-Path -LiteralPath $Dens)) { return }
    foreach ($instance in @("second", "main")) {
        if (Test-Path -LiteralPath (Join-Path $DataRoot $instance)) {
            Invoke-Dens @("uninstall", "--instance", $instance, "--yes") -AllowFailure | Out-Null
        }
    }
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run the harness from an elevated PowerShell (Run as administrator)."
}
if (Test-Installed) {
    throw "Dens is installed on this machine. The harness installs and removes the real service; run it where Dens isn't installed."
}

$passed = $false
try {
    New-Item -ItemType Directory -Path $Work | Out-Null
    $Release = Join-Path $Work "release"
    Copy-Item -LiteralPath (Resolve-Path -LiteralPath $ReleaseDir).ProviderPath -Destination $Release -Recurse
    $Next = Join-Path $Release "next"
    if ($InstallerCandidate) {
        $candidate = (Resolve-Path -LiteralPath $InstallerCandidate).ProviderPath
        Copy-Item -LiteralPath $candidate -Destination (Join-Path $Release "install.ps1") -Force
        Copy-Item -LiteralPath $candidate -Destination (Join-Path $Next "install.ps1") -Force
    }
    $V1 = ([IO.File]::ReadAllText((Join-Path $Release "version"))).Trim()
    $V2 = ([IO.File]::ReadAllText((Join-Path $Next "version"))).Trim()
    $env:APP_SKIP_VERIFY = "true"

    # The installer defaults to the account at the console; name one only
    # where nobody is signed in there, as on a CI runner.
    $me = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $console = (Get-CimInstance Win32_ComputerSystem).UserName
    $userArgs = @()
    if (-not $console -or $console -ne $me) { $userArgs = @("-User", $me) }

    Step "install $V1"
    Invoke-Installer $Release $userArgs
    Assert-Running "dens-main"
    Assert-State "main" $V1
    $config = (Invoke-Native -FilePath "sc.exe" -Arguments @("qc", "dens-main")).Output
    if ($config -notmatch "SERVICE_START_NAME\s*:\s*NT SERVICE\\dens-main") { Fail "the service doesn't run as its virtual account" }
    $sidType = (Invoke-Native -FilePath "sc.exe" -Arguments @("qsidtype", "dens-main")).Output
    if ($sidType -notmatch "SERVICE_SID_TYPE:\s*RESTRICTED") { Fail "the service SID isn't restricted" }
    $privileges = (Invoke-Native -FilePath "sc.exe" -Arguments @("qprivs", "dens-main")).Output
    $held = @([regex]::Matches($privileges, "Se[A-Za-z]+Privilege") | ForEach-Object { $_.Value })
    if ($held.Count -ne 1 -or $held[0] -ne "SeChangeNotifyPrivilege") { Fail "the service keeps privileges: $($held -join ', ')" }
    # The instance root and its data directory don't inherit from ProgramData;
    # the control directory inherits the root's, where the service only reads.
    foreach ($dir in @("main", "main\control", "main\data")) {
        $acl = Get-Acl -LiteralPath (Join-Path $DataRoot $dir)
        if ($dir -ne "main\control" -and -not $acl.AreAccessRulesProtected) { Fail "$dir inherits permissions" }
        $names = @($acl.Access | ForEach-Object { $_.IdentityReference.Value } | Sort-Object -Unique)
        $unexpected = @($names | Where-Object { $_ -notin @("NT AUTHORITY\SYSTEM", "BUILTIN\Administrators", "NT SERVICE\dens-main") })
        if ($unexpected.Count -gt 0) { Fail "$dir grants $($unexpected -join ', ')" }
        $serviceWrites = @($acl.Access | Where-Object {
                $_.IdentityReference.Value -eq "NT SERVICE\dens-main" -and
                ($_.FileSystemRights -band [Security.AccessControl.FileSystemRights]::WriteData) })
        if ($dir -ne "main\data" -and $serviceWrites.Count -gt 0) { Fail "the service can write to $dir" }
    }
    if (-not (Test-OnPath)) { Fail "$BinaryDir isn't on the system PATH" }
    if (-not (Test-WerExcluded)) { Fail "dens.exe isn't excluded from Windows Error Reporting" }

    Step "pair a browser and set the local password"
    Assert-Status "main" "password:\s+not set"
    $url = (Invoke-Dens @("open", "--print")).Output.Trim()
    $token = ($url -split "#token=")[-1]
    $script:Session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    Invoke-Api POST "/api/pair" @{ token = $token } | Out-Null
    if (Invoke-Api POST "/api/pair" @{ token = $token } -AllowFailure) { Fail "a pairing token worked twice" }
    Invoke-Api POST "/api/password" @{ password = $Password } | Out-Null
    $page = Invoke-Api GET "/"
    if ($page.Content -notmatch "Dens is running") { Fail "home page missing after setup" }
    $line = Get-StatusLine "attacker.example"
    if ($line -notmatch " 421 ") { Fail "a foreign Host got '$line'" }

    Step "another account is refused"
    $other = Invoke-AsLocalService
    Write-Host $other.Output
    if ($other.Code -eq "0") { Fail "LOCAL SERVICE reached the control endpoint" }
    if ($other.Output -notmatch "only answers") { Fail "unexpected refusal: $($other.Output)" }

    Step "back up"
    $ownBackup = Join-Path $Work "windows.backup"
    Invoke-Dens @("backup", "--password-stdin", "-o", $ownBackup) -Stdin $Password | Out-Null
    if ($SaveBackup) { Copy-Item -LiteralPath $ownBackup -Destination $SaveBackup -Force }

    Step "the vault and session survive a service restart"
    Invoke-Dens @("service", "restart") | Out-Null
    Wait-Status "main"
    Assert-Status "main" "password:\s+set"
    if ((Invoke-Api GET "/api/status").Content -notmatch '"passwordSet":true') { Fail "the browser session didn't survive" }

    Step "update to $V2 through dens update"
    Invoke-Dens @("update", "--release-url", (ConvertTo-FileUrl $Next)) | Out-Null
    Assert-State "main" $V2
    Wait-Status "main"
    Assert-Status "main" ("version:\s+" + [regex]::Escape($V2))
    Assert-Status "main" "password:\s+set"

    Step "a second instance that hosts a den"
    Invoke-Installer $Next (@("-Instance", "second", "-ClientPort", "18484", "-Den") + $userArgs)
    Assert-Running "dens-second"
    Assert-State "second" $V2
    Assert-State "main" $V2
    Assert-Status "second" "den:\s+hosting a den"
    $health = Invoke-WebRequest -Uri $DenHealth -UseBasicParsing -TimeoutSec 30
    if ($health.StatusCode -ne 200) { Fail "the den listener answered $($health.StatusCode)" }
    if ((Get-MediaRules).Count -ne 2) { Fail "want two media firewall rules for the den" }

    Step "restore a backup into the second instance"
    # A browser paired before the restore must pair again after it.
    $secondUrl = "http://127.0.0.1:18484"
    $secondSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $pairing = (Invoke-Dens @("open", "--print", "--instance", "second")).Output.Trim()
    Invoke-WebRequest -Uri "$secondUrl/api/pair" -Method POST -WebSession $secondSession -UseBasicParsing `
        -Headers @{ Origin = $secondUrl } -ContentType "application/json" `
        -Body (@{ token = ($pairing -split "#token=")[-1] } | ConvertTo-Json -Compress) | Out-Null
    Invoke-WebRequest -Uri "$secondUrl/api/status" -WebSession $secondSession -UseBasicParsing | Out-Null
    $wrong = Invoke-Dens @("restore", $ownBackup, "--instance", "second", "--password-stdin", "--yes") -Stdin "wrong wrong wrong" -AllowFailure
    if ($wrong.Code -eq 0) { Fail "restore accepted the wrong password" }
    Invoke-Dens @("restore", $ownBackup, "--instance", "second", "--password-stdin", "--yes") -Stdin $Password | Out-Null
    Assert-State "second" $V2
    Wait-Status "second"
    Assert-Status "second" "password:\s+set"
    $stillPaired = $true
    try {
        Invoke-WebRequest -Uri "$secondUrl/api/status" -WebSession $secondSession -UseBasicParsing | Out-Null
    } catch {
        $stillPaired = $false
    }
    if ($stillPaired) { Fail "a browser paired before the restore is still paired" }
    if ($Backup) {
        Step "restore the backup made elsewhere"
        $foreign = (Resolve-Path -LiteralPath $Backup).ProviderPath
        Invoke-Dens @("restore", $foreign, "--instance", "second", "--password-stdin", "--yes") -Stdin $Password | Out-Null
        Wait-Status "second"
        Assert-Status "second" "password:\s+set"
    }

    Step "uninstall both instances"
    Invoke-Dens @("uninstall", "--instance", "second", "--yes") | Out-Null
    if (-not (Test-Path -LiteralPath $Dens)) { Fail "uninstalling one instance removed the shared binary" }
    if ((Get-MediaRules).Count -ne 0) { Fail "the den's firewall rules survived its uninstall" }
    Invoke-Dens @("uninstall", "--yes") | Out-Null
    if (Get-Service -Name "dens-*" -ErrorAction SilentlyContinue) { Fail "a service survived uninstall" }
    if (Test-Path -LiteralPath $DataRoot) { Fail "$DataRoot survived uninstall" }
    if (Test-Path -LiteralPath $Dens) { Fail "$Dens survived uninstall" }
    # The uninstaller ran from the binary, so it's set aside for deletion at reboot.
    $left = @(Get-ChildItem -LiteralPath $BinaryDir -ErrorAction SilentlyContinue | Where-Object { $_.Name -notlike "dens.exe.old-*" })
    if ($left.Count -gt 0) { Fail "uninstall left $($left.Name -join ', ') in $BinaryDir" }
    if (Test-OnPath) { Fail "$BinaryDir is still on the system PATH" }
    if (Test-WerExcluded) { Fail "dens.exe is still excluded from Windows Error Reporting" }
    $passed = $true
} catch {
    Write-Host ""
    Write-Host $_.Exception.Message
    foreach ($instance in @("main", "second")) {
        $log = Join-Path $DataRoot "$instance\data\logs"
        if (Test-Path -LiteralPath $log) {
            Get-ChildItem -LiteralPath $log -File | Sort-Object LastWriteTime | Select-Object -Last 1 |
                ForEach-Object { Write-Host "--- $($_.FullName)"; Get-Content -LiteralPath $_.FullName -Tail 30 }
        }
    }
} finally {
    Remove-Installation
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item Env:APP_SKIP_VERIFY -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $Work -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
if ($passed) {
    Write-Host "Windows lifecycle e2e: PASSED"
    exit 0
}
Write-Host "Windows lifecycle e2e: FAILED"
exit 1
