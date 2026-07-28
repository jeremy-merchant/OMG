$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Fail([string]$Message) {
    throw "OMG install: $Message"
}

$Repository = if ($env:OMG_REPOSITORY) { $env:OMG_REPOSITORY } else { "jeremy-merchant/OMG" }
$Version = if ($env:OMG_VERSION) { $env:OMG_VERSION } else { "latest" }
$InstallDir = if ($env:OMG_INSTALL_DIR) { $env:OMG_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "OMG\bin" }
$SourceBinary = $env:OMG_INSTALL_SOURCE
$ExpectedSha = $env:OMG_INSTALL_SHA256

if (-not $env:USERPROFILE) { Fail "USERPROFILE is unavailable" }
if (-not $env:LOCALAPPDATA -and -not $env:OMG_INSTALL_DIR) { Fail "LOCALAPPDATA is unavailable" }

$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("omg-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $TempRoot -Force | Out-Null
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

try {
    $Candidate = Join-Path $TempRoot "omg.exe"
    if ($SourceBinary) {
        if (-not (Test-Path -LiteralPath $SourceBinary -PathType Leaf)) { Fail "OMG_INSTALL_SOURCE is not a regular file" }
        $SourceItem = Get-Item -LiteralPath $SourceBinary -Force
        if ($SourceItem.Attributes -band [IO.FileAttributes]::ReparsePoint) { Fail "OMG_INSTALL_SOURCE must not be a reparse point" }
        if (-not $ExpectedSha) { Fail "OMG_INSTALL_SHA256 is required with OMG_INSTALL_SOURCE" }
        Copy-Item -LiteralPath $SourceBinary -Destination $Candidate -Force
    }
    else {
        $Architecture = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
            "X64" { "amd64" }
            "Arm64" { "arm64" }
            default { Fail "unsupported architecture" }
        }
        $Asset = "omg_windows_${Architecture}.zip"
        $Base = if ($Version -eq "latest") {
            "https://github.com/$Repository/releases/latest/download"
        }
        else {
            "https://github.com/$Repository/releases/download/$Version"
        }
        $Archive = Join-Path $TempRoot $Asset
        $Checksums = Join-Path $TempRoot "checksums.txt"
        Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Asset" -OutFile $Archive
        Invoke-WebRequest -UseBasicParsing -Uri "$Base/checksums.txt" -OutFile $Checksums
        $ChecksumLine = Get-Content -LiteralPath $Checksums | Where-Object { $_ -match "^[a-fA-F0-9]{64}\s+\*?$([regex]::Escape($Asset))$" } | Select-Object -First 1
        if (-not $ChecksumLine) { Fail "release checksum is missing for $Asset" }
        $ArchiveExpected = ($ChecksumLine -split "\s+")[0].ToLowerInvariant()
        $ArchiveActual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Archive).Hash.ToLowerInvariant()
        if ($ArchiveActual -ne $ArchiveExpected) { Fail "release archive checksum mismatch" }
        Expand-Archive -LiteralPath $Archive -DestinationPath $TempRoot -Force
        if (-not (Test-Path -LiteralPath $Candidate -PathType Leaf)) { Fail "release archive does not contain omg.exe" }
        $ExpectedSha = (Get-FileHash -Algorithm SHA256 -LiteralPath $Candidate).Hash.ToLowerInvariant()
    }

    $ActualSha = (Get-FileHash -Algorithm SHA256 -LiteralPath $Candidate).Hash.ToLowerInvariant()
    if ($ActualSha -ne $ExpectedSha.ToLowerInvariant()) { Fail "binary checksum mismatch" }

    $Destination = Join-Path $InstallDir "omg.exe"
    if (Test-Path -LiteralPath $Destination) {
        $DestinationItem = Get-Item -LiteralPath $Destination -Force
        if ($DestinationItem.Attributes -band [IO.FileAttributes]::ReparsePoint) { Fail "refusing to replace reparse-point destination" }
    }
    $Staged = Join-Path $InstallDir (".omg-install-" + [guid]::NewGuid().ToString("N") + ".exe")
    Copy-Item -LiteralPath $Candidate -Destination $Staged -Force
    Move-Item -LiteralPath $Staged -Destination $Destination -Force

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $PathEntries = @($UserPath -split ";" | Where-Object { $_ })
    if (-not ($PathEntries | Where-Object { $_.TrimEnd("\") -ieq $InstallDir.TrimEnd("\") })) {
        $NewUserPath = if ($UserPath) { "$InstallDir;$UserPath" } else { $InstallDir }
        [Environment]::SetEnvironmentVariable("Path", $NewUserPath, "User")
    }
    $env:Path = "$InstallDir;$env:Path"

    & $Destination agent install
    if ($LASTEXITCODE -ne 0) { Fail "agent auto-configuration failed" }

    Write-Host ""
    Write-Host "OMG installed successfully."
    Write-Host "  binary  $Destination"
    Write-Host "  sha256  $ActualSha"
    Write-Host "  agents  configured automatically"
}
finally {
    Remove-Item -LiteralPath $TempRoot -Recurse -Force -ErrorAction SilentlyContinue
}
