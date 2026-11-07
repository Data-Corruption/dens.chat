#Requires -Version 5.1
<#
Dens installer for Windows 11.

Downloads the current release, verifies its signature and checksum, and runs
the release binary's install command as Administrator. That command makes
every change to the system; this script only fetches and verifies.

In an ordinary PowerShell, this line installs Dens, or updates it:
  irm '<RELEASE_URL>install.ps1' | iex
Run as text like that, the script has Windows start Windows PowerShell as
Administrator, once the member allows it, which fetches the script again from
the same release host and runs it in a window of its own; then it opens Dens.
Run as text in an elevated PowerShell, it installs in place.

Run as a file, it takes flags, and needs an elevated PowerShell (Run as
administrator). Download this script and install.ps1.cosign.bundle, verify
them, then run:
  powershell -ExecutionPolicy Bypass -File install.ps1              install, or update
  powershell -ExecutionPolicy Bypass -File install.ps1 -DryRun      show what would change
  powershell -ExecutionPolicy Bypass -File install.ps1 -Instance second
  powershell -ExecutionPolicy Bypass -File install.ps1 -Uninstall   remove an instance and its data

-ReleaseUrl URL installs from a byte-for-byte mirror of the release host; the
signatures still verify. -SkipVerify skips the signature check (the checksum
still applies), for testing unsigned releases only. Only these parameters
change either, never the environment, which an elevated shell inherits from
its user.
#>
[CmdletBinding()]
param(
    [switch]$Update,
    [switch]$Uninstall,
    [switch]$DryRun,
    [string]$Instance = "main",
    [string]$User,
    # v1.0's den options, passed on for dens install to refuse: hosting is
    # turned on and off in Dens's settings.
    [switch]$Den,
    [switch]$NoDen,
    [int]$ClientPort,
    [int]$DenPort,
    [int]$MediaUdpPort,
    [int]$MediaTcpPort,
    [string]$ReleaseUrl,
    [switch]$SkipVerify
)

# The script is this function, called on its last line, so a download cut
# short runs nothing. Run as text, it runs in the caller's own session, so it
# keeps its settings and helpers inside, and never exits, which would close
# the caller's terminal.
function Install-Dens {
    Set-StrictMode -Version Latest
    $ErrorActionPreference = "Stop"
    $ProgressPreference = "SilentlyContinue"

    # Rendered by scripts/build.sh.
    $AppName = "<APP_NAME>"
    $DefaultReleaseUrl = "<RELEASE_URL>"
    $CertificateIdentity = "<CERT_IDENTITY>"
    $OidcIssuer = "<OIDC_ISSUER>"
    $CosignVersion = "<COSIGN_VERSION>"
    $CosignSha256 = "<COSIGN_SHA_WINDOWS_AMD64>"

    $VersionPattern = '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'

    function Invoke-Native {
        param([string]$FilePath, [string[]]$Arguments, [string]$FailureMessage)
        # Windows PowerShell 5.1 turns native stderr into error records, and
        # cosign writes its success messages there; judge by the exit code.
        $saved = $ErrorActionPreference
        try {
            $ErrorActionPreference = "Continue"
            $output = & $FilePath @Arguments 2>&1
            $code = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $saved
        }
        if ($code -ne 0) {
            throw "$FailureMessage (exit $code):`n$(($output | Out-String).Trim())"
        }
    }

    function Get-Download {
        param([string]$Url, [string]$Destination)
        $uri = [Uri]$Url
        if ($uri.Scheme -eq "file") {
            Copy-Item -LiteralPath $uri.LocalPath -Destination $Destination
        } else {
            [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
            Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing -TimeoutSec 300
        }
    }

    function Get-ExpectedHash {
        param([string]$ChecksumsPath, [string]$FileName)
        foreach ($line in [IO.File]::ReadAllLines($ChecksumsPath)) {
            if ($line -match "^\s*([0-9A-Fa-f]{64})\s+\*?(.+?)\s*$" -and $Matches[2] -ceq $FileName) {
                return $Matches[1].ToUpperInvariant()
            }
        }
        throw "checksums.txt has no entry for $FileName."
    }

    function Assert-FileHash {
        param([string]$Path, [string]$Expected)
        $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToUpperInvariant()
        if ($actual -cne $Expected.ToUpperInvariant()) {
            throw "$(Split-Path -Leaf $Path) doesn't match its checksum."
        }
    }

    function Expand-Gzip {
        param([string]$Source, [string]$Destination)
        $in = [IO.File]::OpenRead($Source)
        try {
            $gzip = New-Object IO.Compression.GZipStream($in, [IO.Compression.CompressionMode]::Decompress)
            try {
                $out = [IO.File]::Create($Destination)
                try { $gzip.CopyTo($out) } finally { $out.Dispose() }
            } finally { $gzip.Dispose() }
        } finally { $in.Dispose() }
    }

    # Get-ReleaseRoot is where the release comes from: -ReleaseUrl, or the
    # release host the script was rendered for.
    function Get-ReleaseRoot {
        $root = $ReleaseUrl
        if ([string]::IsNullOrWhiteSpace($root)) { $root = $DefaultReleaseUrl }
        $root = $root.Trim().TrimEnd("/") + "/"
        $scheme = ([Uri]$root).Scheme
        if ($scheme -ne "https" -and $scheme -ne "file") {
            throw "Release URL must use https: $root"
        }
        return $root
    }

    # New-WorkDirectory makes the directory the release is downloaded to,
    # checked in and run from: in ProgramData, which only administrators and
    # SYSTEM may change. The elevated shell's own temporary directory is the
    # member's, where their programs could swap a download between its check
    # and its run, and could rename a directory made private there. Users may
    # add to a new directory in ProgramData until its permissions are set,
    # so it must be empty after.
    function New-WorkDirectory {
        $programData = [Environment]::GetFolderPath("CommonApplicationData")
        $path = Join-Path $programData ("$AppName-install-" + [Guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $path | Out-Null
        $acl = Get-Acl -LiteralPath $path
        $acl.SetSecurityDescriptorSddlForm("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", "Access")
        Set-Acl -LiteralPath $path -AclObject $acl
        $acl = Get-Acl -LiteralPath $path
        $others = @($acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier]) |
                Where-Object { @("S-1-5-18", "S-1-5-32-544") -notcontains $_.IdentityReference.Value })
        if (-not $acl.AreAccessRulesProtected -or $others.Count -gt 0 -or @(Get-ChildItem -LiteralPath $path -Force).Count -gt 0) {
            throw "$path isn't private to administrators; not installing."
        }
        return $path
    }

    # ConvertTo-Literal quotes a value for PowerShell to read back as it is:
    # in single quotes, each quote inside doubled, the typographic ones too,
    # which PowerShell also takes as quotes.
    function ConvertTo-Literal([string]$Value) {
        return "'" + ($Value -replace "['\u2018-\u201B]", '$0$0') + "'"
    }

    # Get-ElevatedCommand is what the elevated Windows PowerShell runs: it
    # fetches this script again from the release host and runs it, as text,
    # with the same flags. It runs nothing the member's account can change:
    # Windows PowerShell starts without a profile, and loads modules only
    # from its own folder, not from the member's documents, which the module
    # path the member's environment hands it lists first. Its window stays
    # open after a failure, whose output can't reach the terminal that
    # started it, and after a dry run or an uninstall, whose output is the
    # point.
    function Get-ElevatedCommand {
        $flags = @("-Instance", (ConvertTo-Literal $Instance))
        if ($Update) { $flags += "-Update" }
        if ($Uninstall) { $flags += "-Uninstall" }
        if ($DryRun) { $flags += "-DryRun" }
        if ($User) { $flags += @("-User", (ConvertTo-Literal $User)) }
        if ($Den) { $flags += "-Den" }
        if ($NoDen) { $flags += "-NoDen" }
        if ($ClientPort) { $flags += @("-ClientPort", $ClientPort) }
        if ($DenPort) { $flags += @("-DenPort", $DenPort) }
        if ($MediaUdpPort) { $flags += @("-MediaUdpPort", $MediaUdpPort) }
        if ($MediaTcpPort) { $flags += @("-MediaTcpPort", $MediaTcpPort) }
        if ($ReleaseUrl) { $flags += @("-ReleaseUrl", (ConvertTo-Literal $ReleaseUrl)) }
        if ($SkipVerify) { $flags += "-SkipVerify" }
        $keep = if ($DryRun -or $Uninstall) { '$true' } else { '$false' }
        $command = @'
$env:PSModulePath = "$PSHOME\Modules"
$ProgressPreference = 'SilentlyContinue'
$failed = $false
try {
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw 'Windows PowerShell did not start as Administrator.'
    }
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $text = (New-Object Net.WebClient).DownloadString(@URL@)
    & ([scriptblock]::Create($text)) @FLAGS@
} catch {
    $failed = $true
    Write-Host ''
    Write-Host $_.Exception.Message -ForegroundColor Red
}
if ($failed -or @KEEP@) {
    Write-Host ''
    Read-Host 'Press Enter to close' | Out-Null
}
if ($failed) { exit 1 }
exit 0
'@
        return $command.Replace("@URL@", (ConvertTo-Literal ((Get-ReleaseRoot) + "install.ps1"))).Replace("@FLAGS@", ($flags -join " ")).Replace("@KEEP@", $keep)
    }

    # Invoke-Elevated runs the script again as Administrator, for a run as
    # text from an ordinary PowerShell: Windows asks the member to let
    # Windows PowerShell make changes, and it installs in a window of its
    # own. Then this terminal, still the member's, opens Dens.
    function Invoke-Elevated {
        $command = Get-ElevatedCommand
        $start = New-Object Diagnostics.ProcessStartInfo
        $start.FileName = Join-Path ([Environment]::SystemDirectory) "WindowsPowerShell\v1.0\powershell.exe"
        $start.Arguments = "-NoProfile -EncodedCommand " + [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
        $start.Verb = "runas"
        $start.UseShellExecute = $true
        Write-Host "Starting the installer as Administrator, in a window of its own ..."
        try {
            $process = [Diagnostics.Process]::Start($start)
        } catch {
            for ($e = $_.Exception; $null -ne $e; $e = $e.InnerException) {
                # ERROR_CANCELLED: Windows' prompt was declined.
                if ($e -is [ComponentModel.Win32Exception] -and $e.NativeErrorCode -eq 1223) {
                    Write-Host "Windows' prompt was declined, so nothing changed."
                    return
                }
            }
            throw
        }
        if ($null -eq $process) { throw "Windows PowerShell didn't start." }
        $process.WaitForExit()
        if ($process.ExitCode -ne 0) {
            Write-Host "The installer failed; its window said why." -ForegroundColor Red
            return
        }
        if ($DryRun -or $Uninstall) { return }
        # By its path: this terminal's PATH predates the install.
        $open = @("open")
        if ($Instance -ne "main") { $open += @("--instance", $Instance) }
        if (Test-Path -LiteralPath $Installed -PathType Leaf) {
            & $Installed @open
        } else {
            Write-Host "Open $AppTitle from the Start menu."
        }
    }

    $AppTitle = $AppName.Substring(0, 1).ToUpperInvariant() + $AppName.Substring(1)
    # Known folders, not the environment, which an elevated shell inherits
    # from its user.
    $Installed = Join-Path (Join-Path ([Environment]::GetFolderPath("ProgramFiles")) $AppTitle) "$AppName.exe"

    # Run as a file, the script has a path; run as text, through iex or as a
    # script block, it has none.
    $asText = [string]::IsNullOrEmpty($PSCommandPath)
    $principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        if (-not $asText) {
            throw "The installer changes the system; run it from an elevated PowerShell (Run as administrator)."
        }
        Invoke-Elevated
        return
    }

    if ($Uninstall) {
        if (-not (Test-Path -LiteralPath $Installed -PathType Leaf)) {
            throw "$AppName isn't installed; nothing to uninstall."
        }
        $command = "uninstall"
        $arguments = @("uninstall", "--instance", $Instance)
        if ($DryRun) { $arguments += "--dry-run" }
        & $Installed @arguments
        $status = $LASTEXITCODE
    } else {
        $command = "install"
        $releaseRoot = Get-ReleaseRoot

        $architecture = $env:PROCESSOR_ARCHITEW6432
        if ([string]::IsNullOrWhiteSpace($architecture)) { $architecture = $env:PROCESSOR_ARCHITECTURE }
        switch ($architecture.ToUpperInvariant()) {
            "AMD64" { $arch = "amd64" }
            "ARM64" { $arch = "arm64" }
            default { throw "Unsupported architecture $architecture; Dens runs on AMD64 and ARM64." }
        }

        $Temp = New-WorkDirectory
        try {
            # Read the version pointer once and pin every other download to it.
            Get-Download -Url ($releaseRoot + "version") -Destination (Join-Path $Temp "pointer")
            $version = ([IO.File]::ReadAllText((Join-Path $Temp "pointer"))).Trim()
            if ($version -cnotmatch $VersionPattern) {
                throw "The release host returned an invalid version: '$version'."
            }
            $release = $releaseRoot + "releases/$version/"
            $asset = "windows-$arch.exe.gz"
            Write-Host "Downloading $AppName $version ..."
            Get-Download -Url ($release + $asset) -Destination (Join-Path $Temp $asset)
            Get-Download -Url ($release + "version") -Destination (Join-Path $Temp "version")
            Get-Download -Url ($release + "checksums.txt") -Destination (Join-Path $Temp "checksums.txt")

            $cosign = $null
            if ($SkipVerify) {
                Write-Warning "-SkipVerify: NOT verifying the release signature. Only for testing."
            } else {
                Write-Host "Verifying the release signature ..."
                Get-Download -Url ($release + "checksums.txt.cosign.bundle") -Destination (Join-Path $Temp "checksums.txt.cosign.bundle")
                # cosign publishes no windows-arm64 build; the amd64 one runs under emulation.
                $cosign = Join-Path $Temp "cosign.exe"
                Get-Download -Url "https://github.com/sigstore/cosign/releases/download/$CosignVersion/cosign-windows-amd64.exe" -Destination $cosign
                Assert-FileHash -Path $cosign -Expected $CosignSha256
                Invoke-Native -FilePath $cosign -FailureMessage "The release signature doesn't verify; not installing" -Arguments @(
                    "verify-blob",
                    "--bundle", (Join-Path $Temp "checksums.txt.cosign.bundle"),
                    "--certificate-identity", $CertificateIdentity,
                    "--certificate-oidc-issuer", $OidcIssuer,
                    (Join-Path $Temp "checksums.txt"))
            }

            $checksums = Join-Path $Temp "checksums.txt"
            Assert-FileHash -Path (Join-Path $Temp $asset) -Expected (Get-ExpectedHash -ChecksumsPath $checksums -FileName $asset)
            Assert-FileHash -Path (Join-Path $Temp "version") -Expected (Get-ExpectedHash -ChecksumsPath $checksums -FileName "version")
            if (([IO.File]::ReadAllText((Join-Path $Temp "version"))).Trim() -cne $version) {
                throw "The signed release is not version $version."
            }

            $binary = Join-Path $Temp "$AppName.exe"
            Expand-Gzip -Source (Join-Path $Temp $asset) -Destination $binary

            $arguments = @("install", "--instance", $Instance, "--release-url", $releaseRoot)
            if ($cosign) { $arguments += @("--cosign", $cosign) }
            if ($User) { $arguments += @("--user", $User) }
            if ($Den) { $arguments += "--den" }
            if ($NoDen) { $arguments += "--den=false" }
            if ($ClientPort) { $arguments += @("--client-port", $ClientPort) }
            if ($DenPort) { $arguments += @("--den-port", $DenPort) }
            if ($MediaUdpPort) { $arguments += @("--media-udp-port", $MediaUdpPort) }
            if ($MediaTcpPort) { $arguments += @("--media-tcp-port", $MediaTcpPort) }
            if ($DryRun) { $arguments += "--dry-run" }
            & $binary @arguments
            $status = $LASTEXITCODE
        } finally {
            Remove-Item -LiteralPath $Temp -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

    # Run as a file, the script exits with the command's status, which dens
    # update and the harnesses read; run as text, it throws on a failure.
    if (-not $asText) { exit $status }
    if ($status -ne 0) { throw "$AppName $command failed (exit $status)." }
    if (-not $Uninstall -and -not $DryRun) {
        # Not dens open, which here would open the browser as Administrator.
        Write-Host "Open $AppTitle from the Start menu."
    }
}

Install-Dens
