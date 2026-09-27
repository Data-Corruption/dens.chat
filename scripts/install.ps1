#Requires -Version 5.1
<#
Dens installer for Windows 11.

Downloads the current release, verifies its signature and checksum, and runs
the release binary's install command as Administrator. That command makes
every change to the system; this script only fetches and verifies.

Download this script and install.ps1.cosign.bundle, verify them, then run
from an elevated PowerShell (Run as administrator):
  powershell -ExecutionPolicy Bypass -File install.ps1              install, or update
  powershell -ExecutionPolicy Bypass -File install.ps1 -DryRun      show what would change
  powershell -ExecutionPolicy Bypass -File install.ps1 -Instance second -Den
  powershell -ExecutionPolicy Bypass -File install.ps1 -Uninstall   remove an instance and its data

APP_RELEASE_URL installs from a byte-for-byte mirror of the release host; the
signatures still verify. APP_SKIP_VERIFY=true skips the signature check (the
checksum still applies), for testing unsigned releases only.
#>
[CmdletBinding()]
param(
    [switch]$Update,
    [switch]$Uninstall,
    [switch]$DryRun,
    [string]$Instance = "main",
    [string]$User,
    [switch]$Den,
    [switch]$NoDen,
    [int]$ClientPort,
    [int]$DenPort,
    [int]$MediaUdpPort,
    [int]$MediaTcpPort
)

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

$SkipVerify = ($env:APP_SKIP_VERIFY -eq "true" -or $env:APP_SKIP_VERIFY -eq "1")
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

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "The installer changes the system; run it from an elevated PowerShell (Run as administrator)."
}

$AppTitle = $AppName.Substring(0, 1).ToUpperInvariant() + $AppName.Substring(1)
$Installed = Join-Path (Join-Path $env:ProgramFiles $AppTitle) "$AppName.exe"

if ($Uninstall) {
    if (-not (Test-Path -LiteralPath $Installed -PathType Leaf)) {
        throw "$AppName isn't installed; nothing to uninstall."
    }
    $arguments = @("uninstall", "--instance", $Instance)
    if ($DryRun) { $arguments += "--dry-run" }
    & $Installed @arguments
    exit $LASTEXITCODE
}

$ReleaseUrl = $DefaultReleaseUrl
if (-not [string]::IsNullOrWhiteSpace($env:APP_RELEASE_URL)) { $ReleaseUrl = $env:APP_RELEASE_URL }
$ReleaseUrl = $ReleaseUrl.Trim().TrimEnd("/") + "/"
$scheme = ([Uri]$ReleaseUrl).Scheme
if ($scheme -ne "https" -and $scheme -ne "file") {
    throw "Release URL must use https: $ReleaseUrl"
}

$architecture = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrWhiteSpace($architecture)) { $architecture = $env:PROCESSOR_ARCHITECTURE }
switch ($architecture.ToUpperInvariant()) {
    "AMD64" { $arch = "amd64" }
    "ARM64" { $arch = "arm64" }
    default { throw "Unsupported architecture $architecture; Dens runs on AMD64 and ARM64." }
}

$Temp = Join-Path ([IO.Path]::GetTempPath()) ("dens-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $Temp | Out-Null
try {
    # Read the version pointer once and pin every other download to it.
    Get-Download -Url ($ReleaseUrl + "version") -Destination (Join-Path $Temp "pointer")
    $version = ([IO.File]::ReadAllText((Join-Path $Temp "pointer"))).Trim()
    if ($version -cnotmatch $VersionPattern) {
        throw "The release host returned an invalid version: '$version'."
    }
    $release = $ReleaseUrl + "releases/$version/"
    $asset = "windows-$arch.exe.gz"
    Write-Host "Downloading $AppName $version ..."
    Get-Download -Url ($release + $asset) -Destination (Join-Path $Temp $asset)
    Get-Download -Url ($release + "version") -Destination (Join-Path $Temp "version")
    Get-Download -Url ($release + "checksums.txt") -Destination (Join-Path $Temp "checksums.txt")

    $cosign = $null
    if ($SkipVerify) {
        Write-Warning "APP_SKIP_VERIFY is set: NOT verifying the release signature. Only for testing."
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

    $arguments = @("install", "--instance", $Instance, "--release-url", $ReleaseUrl)
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
exit $status
