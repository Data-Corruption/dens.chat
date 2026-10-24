#Requires -Version 5.1
<#
Windows lifecycle e2e. Installs Dens as a service through install.ps1 from
fixture releases and runs the flows the Linux harness runs: host a den with
Dens's first release and update it to this one, which still hosts it (M9.1);
then pair a browser, set the password, turn hosting on and off with dens
den hosting, back up, refuse another account, restart the service, update,
add a second instance that hosts a den, restore, and uninstall.

It installs the real service and paths, so it refuses to run where Dens is
installed. From an elevated PowerShell:
  bash scripts/test/fixture-releases.sh out/windows-e2e windows-amd64
  powershell -ExecutionPolicy Bypass -File scripts/test-lifecycle-e2e.ps1 -ReleaseDir out/windows-e2e

-InstallerCandidate installs with an exact rendered install.ps1 instead of
the fixture's. -Backup also restores a backup made elsewhere, such as by the
Linux harness, with the same test password. -SaveBackup keeps the backup
this run makes. A restarted service stands in for a reboot.

-ClientPort and -SecondClientPort move the two instances' client ports off
the defaults (8484 and 18484), for a machine where those are taken.

On failure it prints the services' logs and event log entries, then
uninstalls. -KeepOnFailure leaves the failed installation in place for
inspection; remove it afterwards with "dens uninstall --yes" (and
"--instance second" if it exists) from an elevated terminal.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ReleaseDir,
    [string]$InstallerCandidate = "",
    [string]$Backup = "",
    [string]$SaveBackup = "",
    [int]$ClientPort = 8484,
    [int]$SecondClientPort = 18484,
    [switch]$KeepOnFailure
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$Password = "correct horse battery staple"
$ClientUrl = "http://127.0.0.1:$ClientPort"
$DenHealth = "http://127.0.0.1:8485/healthz"
$BinaryDir = Join-Path $env:ProgramFiles "Dens"
$Dens = Join-Path $BinaryDir "dens.exe"
$DataRoot = Join-Path $env:ProgramData "Dens"
$RunId = [Guid]::NewGuid().ToString("N").Substring(0, 8)
# Outside every user profile, so the other-account task can reach it.
$Work = Join-Path $env:ProgramData "dens-e2e-$RunId"
$TaskName = "dens-e2e-$RunId"
$WerKey = "HKLM:\SOFTWARE\Microsoft\Windows\Windows Error Reporting\ExcludedApplications"
# The shortcut helper's task for the desktop user, this account (M7).
$HelperTask = "Dens shortcuts-" + [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
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

# Invoke-Installer installs the fixture release at Root, which is unsigned.
function Invoke-Installer {
    param([string]$Root, [string[]]$Arguments)
    $script = Join-Path $Root "install.ps1"
    Invoke-Native -FilePath "powershell.exe" -Arguments (@("-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
            "-File", $script, "-ReleaseUrl", (ConvertTo-FileUrl $Root), "-SkipVerify") + $Arguments) | Out-Null
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
    $client = New-Object Net.Sockets.TcpClient("127.0.0.1", $ClientPort)
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

function Get-HelperTask {
    return Get-ScheduledTask -TaskPath "\" -TaskName $HelperTask -ErrorAction SilentlyContinue
}

function Test-EventSource([string]$Service) {
    return Test-Path -LiteralPath "HKLM:\SYSTEM\CurrentControlSet\Services\EventLog\Application\$Service"
}

# Show-Diagnostics prints what each installed instance and the service
# manager recorded, before cleanup removes it.
function Show-Diagnostics {
    foreach ($instance in @("main", "second")) {
        $root = Join-Path $DataRoot $instance
        if (-not (Test-Path -LiteralPath $root)) { continue }
        $files = @(Get-ChildItem -LiteralPath (Join-Path $root "data\logs") -Filter "*.log" -File -ErrorAction SilentlyContinue)
        $files += @(Get-Item -LiteralPath (Join-Path $root "control\maintenance.log") -ErrorAction SilentlyContinue)
        foreach ($file in $files) {
            Write-Host "--- $($file.FullName)"
            Get-Content -LiteralPath $file.FullName -Tail 40 | ForEach-Object { Write-Host $_ }
        }
        Get-WinEvent -FilterHashtable @{ LogName = "Application"; ProviderName = "dens-$instance" } -MaxEvents 5 -ErrorAction SilentlyContinue |
            ForEach-Object { Write-Host "--- Application event, dens-$instance, $($_.TimeCreated): $($_.Message)" }
    }
    Get-WinEvent -FilterHashtable @{ LogName = "System"; ProviderName = "Service Control Manager" } -MaxEvents 50 -ErrorAction SilentlyContinue |
        Where-Object { $_.Message -like "*Dens (*" } | Select-Object -First 5 |
        ForEach-Object { Write-Host "--- System event $($_.Id), $($_.TimeCreated): $($_.Message)" }
}

function Test-WerExcluded {
    $key = Get-Item -LiteralPath $WerKey -ErrorAction SilentlyContinue
    return ($null -ne $key) -and ($key.GetValueNames() -contains "dens.exe")
}

# Get-InstallerRules finds firewall rules for a temporary installer's
# dens.exe. Windows adds one when it asks the user about a process that
# listens on a non-loopback address and the user answers, which the
# installer must never cause.
function Get-InstallerRules {
    return @(Get-NetFirewallApplicationFilter -ErrorAction SilentlyContinue |
            Where-Object { $_.Program -like "*\dens-install-*" })
}

function Get-MediaRules {
    return @(Get-NetFirewallRule -ErrorAction SilentlyContinue |
            Where-Object { $_.DisplayName -like "Dens (*) media *" })
}

# Test-DenAnswers says whether a den listener answers on the den port.
function Test-DenAnswers {
    try {
        return (Invoke-WebRequest -Uri $DenHealth -UseBasicParsing -TimeoutSec 10).StatusCode -eq 200
    } catch {
        return $false
    }
}

# Wait-OwnConnected waits for the page to show the den this instance hosts
# as connected.
function Wait-OwnConnected {
    $deadline = (Get-Date).AddSeconds(60)
    while ($true) {
        $dens = @(((Invoke-Api GET "/api/dens").Content | ConvertFrom-Json).dens)
        $own = @($dens | Where-Object { $_.own -and $_.state -eq "connected" })
        if ($own.Count -gt 0) { return }
        if ((Get-Date) -gt $deadline) { Fail "the den this instance hosts isn't connected" }
        Start-Sleep -Milliseconds 500
    }
}

# Connect-Browser pairs the main instance's browser session and sets the
# local password.
function Connect-Browser {
    $url = (Invoke-Dens @("open", "--print")).Output.Trim()
    $script:Session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    Invoke-Api POST "/api/pair" @{ token = ($url -split "#token=")[-1] } | Out-Null
    Invoke-Api POST "/api/password" @{ password = $Password } | Out-Null
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

    # The installer defaults to the account at the console; name one only
    # where nobody is signed in there, as on a CI runner.
    $me = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $console = (Get-CimInstance Win32_ComputerSystem).UserName
    $userArgs = @()
    if (-not $console -or $console -ne $me) { $userArgs = @("-User", $me) }

    $installerRules = @(Get-InstallerRules).Count

    # Dens's first release hosts a den, as its installer's den option did,
    # and this one, installed over it, still hosts it: the switch takes the
    # den role it had (M9.1). Then it goes, for the fresh install after it.
    $First = Join-Path $Release "first"
    $FirstVersion = ([IO.File]::ReadAllText((Join-Path $First "version"))).Trim()
    Step "host a den with $FirstVersion"
    $firstArgs = @("install", "--den", "--release-url", (ConvertTo-FileUrl $Release))
    if ($userArgs.Count -gt 0) { $firstArgs += @("--user", $me) }
    if ($ClientPort -ne 8484) { $firstArgs += @("--client-port", $ClientPort) }
    Invoke-Native -FilePath (Join-Path $First "windows-amd64.exe") -Arguments $firstArgs | Out-Null
    Assert-Running "dens-main"
    Assert-State "main" $FirstVersion
    Connect-Browser
    Invoke-Api POST "/api/den" @{
        name = "First Den"; url = "https://den.test"; username = "alice"; display_name = "Alice"; password = "den password"
    } | Out-Null
    Wait-OwnConnected
    if (-not (Test-DenAnswers)) { Fail "$FirstVersion's den listener doesn't answer" }

    Step "update the den $FirstVersion hosts to $V1"
    $mainArgs = $userArgs
    if ($ClientPort -ne 8484) { $mainArgs += @("-ClientPort", $ClientPort) }
    Invoke-Installer $Release $mainArgs
    Assert-State "main" $V1
    Wait-Status "main"
    Assert-Status "main" ("version:\s+" + [regex]::Escape($V1))
    Assert-Status "main" "den:\s+hosting a den"
    if (-not (Test-DenAnswers)) { Fail "the den listener doesn't answer after the update" }
    if ((Invoke-Api GET "/api/hosting").Content -notmatch '"enabled":true,"serving":true') { Fail "hosting isn't on after the update" }
    if ((Invoke-Api GET "/api/dens").Content -notmatch '"name":"First Den"') { Fail "the den is gone after the update" }
    Wait-OwnConnected
    Invoke-Dens @("uninstall", "--yes") | Out-Null
    if (Test-Installed) { Fail "uninstalling the updated instance left it installed" }
    if (@(Get-MediaRules).Count -ne 0) { Fail "the media ports' firewall rules survived uninstall" }
    $script:Session = $null

    Step "the installer refuses v1.0's den option"
    $refused = Invoke-Native -FilePath "powershell.exe" -AllowFailure -Arguments @("-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
        "-File", (Join-Path $Release "install.ps1"), "-ReleaseUrl", (ConvertTo-FileUrl $Release), "-SkipVerify", "-Den")
    if ($refused.Code -eq 0) { Fail "the installer took -Den" }
    if ($refused.Output -notmatch "hosting is turned on and off in Dens's settings") { Fail "unexpected refusal of -Den" }
    if (Test-Installed) { Fail "the refused install changed the system" }

    Step "install $V1"
    Invoke-Installer $Release $mainArgs
    Assert-Running "dens-main"
    Assert-State "main" $V1
    $config = (Invoke-Native -FilePath "sc.exe" -Arguments @("qc", "dens-main")).Output
    if ($config -notmatch "SERVICE_START_NAME\s*:\s*NT SERVICE\\dens-main") { Fail "the service doesn't run as its virtual account" }
    $sidType = (Invoke-Native -FilePath "sc.exe" -Arguments @("qsidtype", "dens-main")).Output
    if ($sidType -notmatch "SERVICE_SID_TYPE:\s*RESTRICTED") { Fail "the service SID isn't restricted" }
    $privileges = (Invoke-Native -FilePath "sc.exe" -Arguments @("qprivs", "dens-main")).Output
    $held = @([regex]::Matches($privileges, "Se[A-Za-z]+Privilege") | ForEach-Object { $_.Value })
    if ($held.Count -ne 1 -or $held[0] -ne "SeChangeNotifyPrivilege") { Fail "the service keeps privileges: $($held -join ', ')" }
    $writeRights = [Security.AccessControl.FileSystemRights]("WriteData, AppendData, WriteExtendedAttributes, " +
        "DeleteSubdirectoriesAndFiles, WriteAttributes, Delete, ChangePermissions, TakeOwnership")
    # The directory holding the instance roots: Users may list it, as with
    # /var/lib/dens on Linux, and only SYSTEM and Administrators may change it.
    $baseAcl = Get-Acl -LiteralPath $DataRoot
    if (-not $baseAcl.AreAccessRulesProtected) { Fail "$DataRoot inherits permissions" }
    foreach ($rule in $baseAcl.Access) {
        $name = $rule.IdentityReference.Value
        if ($name -in @("NT AUTHORITY\SYSTEM", "BUILTIN\Administrators")) { continue }
        if ($name -ne "BUILTIN\Users" -or ($rule.FileSystemRights -band $writeRights)) {
            Fail "$DataRoot grants $name $($rule.FileSystemRights)"
        }
    }
    # The instance root and its data directory don't inherit from ProgramData;
    # the control directory inherits the root's, where the service only reads.
    foreach ($dir in @("main", "main\control", "main\data")) {
        $acl = Get-Acl -LiteralPath (Join-Path $DataRoot $dir)
        if ($dir -ne "main\control" -and -not $acl.AreAccessRulesProtected) { Fail "$dir inherits permissions" }
        $names = @($acl.Access | ForEach-Object { $_.IdentityReference.Value } | Sort-Object -Unique)
        $unexpected = @($names | Where-Object { $_ -notin @("NT AUTHORITY\SYSTEM", "BUILTIN\Administrators", "NT SERVICE\dens-main") })
        if ($unexpected.Count -gt 0) { Fail "$dir grants $($unexpected -join ', ')" }
        $serviceWrites = @($acl.Access | Where-Object {
                $_.IdentityReference.Value -eq "NT SERVICE\dens-main" -and ($_.FileSystemRights -band $writeRights) })
        if ($dir -ne "main\data" -and $serviceWrites.Count -gt 0) { Fail "the service can write to $dir" }
    }
    if (-not (Test-OnPath)) { Fail "$BinaryDir isn't on the system PATH" }
    if (-not (Test-WerExcluded)) { Fail "dens.exe isn't excluded from Windows Error Reporting" }
    # The shortcut helper starts at the desktop user's sign-in, as them, at
    # normal priority and with no time limit.
    $task = Get-HelperTask
    if ($null -eq $task) { Fail "no scheduled task starts the shortcut helper" }
    $actions = @($task.Actions)
    if ($actions.Count -ne 1 -or $actions[0].Execute -ne $Dens -or $actions[0].Arguments -ne "shortcuts") {
        Fail "the shortcut helper's task runs something else"
    }
    if ($task.Principal.UserId -notmatch [regex]::Escape(($me -split "\\")[-1]) -and $task.Principal.UserId -ne [Security.Principal.WindowsIdentity]::GetCurrent().User.Value) {
        Fail "the shortcut helper's task runs as $($task.Principal.UserId)"
    }
    if ("$($task.Principal.LogonType)" -ne "Interactive" -or "$($task.Principal.RunLevel)" -ne "Limited") {
        Fail "the shortcut helper's task runs as $($task.Principal.LogonType), $($task.Principal.RunLevel)"
    }
    if ($task.Settings.Priority -ne 5 -or $task.Settings.ExecutionTimeLimit -ne "PT0S") {
        Fail "the shortcut helper's task has priority $($task.Settings.Priority) and limit $($task.Settings.ExecutionTimeLimit)"
    }
    if (-not (Test-EventSource "dens-main")) { Fail "dens-main isn't registered as an event log source" }

    Step "pair a browser and set the local password"
    Assert-Status "main" "password:\s+not set"
    $url = (Invoke-Dens @("open", "--print")).Output.Trim()
    $token = ($url -split "#token=")[-1]
    $script:Session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    Invoke-Api POST "/api/pair" @{ token = $token } | Out-Null
    if (Invoke-Api POST "/api/pair" @{ token = $token } -AllowFailure) { Fail "a pairing token worked twice" }
    Invoke-Api POST "/api/password" @{ password = $Password } | Out-Null
    $page = Invoke-Api GET "/"
    if ($page.Content -notmatch '<div id="app"') { Fail "the page shell is missing" }
    if ((Invoke-Api GET "/api/status").Content -notmatch '"passwordSet":true') { Fail "the password isn't set after setup" }
    $line = Get-StatusLine "attacker.example"
    if ($line -notmatch " 421 ") { Fail "a foreign Host got '$line'" }

    Step "an install hosts nothing until hosting is on"
    Assert-Status "main" "den:\s+not hosting a den"
    if (Test-DenAnswers) { Fail "the den listener answers with hosting off" }
    # Every install lets the media ports in, for whenever hosting is on,
    # for dens.exe alone and on those ports alone.
    $rules = @(Get-MediaRules)
    if ($rules.Count -ne 2) { Fail "want the media ports' two firewall rules after the install" }
    foreach ($rule in $rules) {
        $program = ($rule | Get-NetFirewallApplicationFilter).Program
        $filter = $rule | Get-NetFirewallPortFilter
        $port = if ("$($filter.Protocol)" -eq "UDP") { "7881" } else { "7882" }
        if ($program -ne $Dens -or "$($filter.LocalPort)" -ne $port -or "$($rule.Direction)" -ne "Inbound" -or "$($rule.Action)" -ne "Allow") {
            Fail "the rule $($rule.DisplayName) lets in $program on $($filter.Protocol) $($filter.LocalPort)"
        }
    }
    # dens den hosting turns it on and off, as the page's switch does (M10).
    $wrongOn = Invoke-Dens @("den", "hosting", "on", "--password-stdin") -Stdin "wrong wrong wrong" -AllowFailure
    if ($wrongOn.Code -eq 0) { Fail "hosting turned on with the wrong password" }
    $on = Invoke-Dens @("den", "hosting", "on", "--password-stdin") -Stdin $Password
    if ($on.Output -notmatch "127\.0\.0\.1:8485") { Fail "turning hosting on doesn't say where Caddy sends the den: $($on.Output)" }
    if (-not (Test-DenAnswers)) { Fail "the den listener doesn't answer with hosting on" }
    Assert-Status "main" "den:\s+hosting a den"
    if ((Invoke-Api GET "/api/hosting").Content -notmatch '"enabled":true,"serving":true') { Fail "the page doesn't see hosting on" }
    $off = Invoke-Dens @("den", "hosting", "off", "--yes")
    if ($off.Output -notmatch "Hosting is off\.") { Fail "hosting didn't turn off: $($off.Output)" }
    if (Test-DenAnswers) { Fail "the den listener answers after hosting turned off" }
    if ((Invoke-Api GET "/api/hosting").Content -notmatch '"enabled":false,"serving":false') { Fail "the page doesn't see hosting off" }

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
    Invoke-Dens @("update", "--release-url", (ConvertTo-FileUrl $Next), "--skip-verify") | Out-Null
    Assert-State "main" $V2
    Wait-Status "main"
    Assert-Status "main" ("version:\s+" + [regex]::Escape($V2))
    Assert-Status "main" "password:\s+set"

    Step "a second instance that hosts a den"
    Invoke-Installer $Next (@("-Instance", "second", "-ClientPort", $SecondClientPort) + $userArgs)
    Assert-Running "dens-second"
    Assert-State "second" $V2
    Assert-State "main" $V2
    $secondUrl = "http://127.0.0.1:$SecondClientPort"
    $secondSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $secondPost = @{ Method = "POST"; WebSession = $secondSession; UseBasicParsing = $true; Headers = @{ Origin = $secondUrl };
        ContentType = "application/json"; TimeoutSec = 30 }
    $pairing = (Invoke-Dens @("open", "--print", "--instance", "second")).Output.Trim()
    Invoke-WebRequest @secondPost -Uri "$secondUrl/api/pair" -Body (@{ token = ($pairing -split "#token=")[-1] } | ConvertTo-Json -Compress) | Out-Null
    Invoke-WebRequest @secondPost -Uri "$secondUrl/api/password" -Body (@{ password = $Password } | ConvertTo-Json -Compress) | Out-Null
    Invoke-Dens @("den", "hosting", "on", "--password-stdin", "--instance", "second") -Stdin $Password | Out-Null
    Assert-Status "second" "den:\s+hosting a den"
    if (-not (Test-DenAnswers)) { Fail "the den listener doesn't answer" }
    if (@(Get-MediaRules).Count -ne 4) { Fail "want each instance's two media firewall rules" }
    if (@(Get-InstallerRules).Count -ne $installerRules) { Fail "Windows Firewall asked about the temporary installer" }

    Step "restore a backup into the second instance"
    # A browser paired before the restore must pair again after it.
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
    # Hosting is the machine's choice, not the backup's (M9.1).
    Assert-Status "second" "den:\s+not hosting a den"
    if (Test-DenAnswers) { Fail "the den listener answers after the restore" }
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
    if ($null -eq (Get-HelperTask)) { Fail "uninstalling one instance removed the shortcut helper's task" }
    if (@(Get-MediaRules).Count -ne 2) { Fail "want the main instance's two media firewall rules alone after uninstalling second" }
    Invoke-Dens @("uninstall", "--yes") | Out-Null
    if (@(Get-MediaRules).Count -ne 0) { Fail "the media ports' firewall rules survived uninstall" }
    if (Get-Service -Name "dens-*" -ErrorAction SilentlyContinue) { Fail "a service survived uninstall" }
    if (Test-Path -LiteralPath $DataRoot) { Fail "$DataRoot survived uninstall" }
    if (Test-Path -LiteralPath $Dens) { Fail "$Dens survived uninstall" }
    # The uninstaller ran from the binary, so it's set aside for deletion at reboot.
    $left = @(Get-ChildItem -LiteralPath $BinaryDir -ErrorAction SilentlyContinue | Where-Object { $_.Name -notlike "dens.exe.old-*" })
    if ($left.Count -gt 0) { Fail "uninstall left $($left.Name -join ', ') in $BinaryDir" }
    if (Test-OnPath) { Fail "$BinaryDir is still on the system PATH" }
    if (Test-WerExcluded) { Fail "dens.exe is still excluded from Windows Error Reporting" }
    if ($null -ne (Get-HelperTask)) { Fail "the shortcut helper's task survived uninstall" }
    foreach ($service in @("dens-main", "dens-second")) {
        if (Test-EventSource $service) { Fail "the $service event log source survived uninstall" }
    }
    $passed = $true
} catch {
    Write-Host ""
    Write-Host $_.Exception.Message
    Show-Diagnostics
} finally {
    if ($passed -or -not $KeepOnFailure) {
        Remove-Installation
    } else {
        Write-Host ""
        Write-Host "Kept the failed installation (-KeepOnFailure)."
    }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $Work -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
if ($passed) {
    Write-Host "Windows lifecycle e2e: PASSED"
    exit 0
}
Write-Host "Windows lifecycle e2e: FAILED"
exit 1
