[CmdletBinding()]
param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = '0.4.0',

    [string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'CaidoBridge.Common.ps1')

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repositoryRoot 'dist'
}
$outputFull = [IO.Path]::GetFullPath($OutputDirectory)
$expectedDist = [IO.Path]::GetFullPath((Join-Path $repositoryRoot 'dist'))
if (-not [string]::Equals($outputFull, $expectedDist, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Release output must be the repository dist directory.'
}
New-Item -ItemType Directory -Force -Path $outputFull | Out-Null

$archiveStem = "CaidoBridge-v$Version-windows-amd64"
$zipPath = Join-Path $outputFull ($archiveStem + '.zip')
$exeSidecar = Join-Path $outputFull 'CaidoBridge.exe.sha256'
$legacyExeSidecar = Join-Path $outputFull 'ph-caido-mcp.exe.sha256'
$zipSidecar = $zipPath + '.sha256'
foreach ($output in @($zipPath, $exeSidecar, $legacyExeSidecar, $zipSidecar)) {
    Remove-Item -LiteralPath $output -Force -ErrorAction SilentlyContinue
}

$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ('caidobridge-release-' + [Guid]::NewGuid().ToString('N'))
$stage = Join-Path $temporaryRoot $archiveStem
$verify = Join-Path $temporaryRoot 'verify'
try {
    New-Item -ItemType Directory -Path (Join-Path $stage 'bin') -Force | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $stage 'scripts') -Force | Out-Null

    $oldCgo = $env:CGO_ENABLED
    $oldGoos = $env:GOOS
    $oldGoarch = $env:GOARCH
    $oldCache = $env:GOCACHE
    try {
        $env:CGO_ENABLED = '0'
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:GOCACHE = Join-Path $temporaryRoot 'go-cache'
        Push-Location $repositoryRoot
        try {
            & go build -buildvcs=false -trimpath -ldflags='-s -w' `
                -o (Join-Path $stage 'bin\CaidoBridge.exe') .\cmd\caidobridge
            if ($LASTEXITCODE -ne 0) { throw 'Windows amd64 build failed.' }
        } finally {
            Pop-Location
        }
    } finally {
        $env:CGO_ENABLED = $oldCgo
        $env:GOOS = $oldGoos
        $env:GOARCH = $oldGoarch
        $env:GOCACHE = $oldCache
    }

    $binary = Join-Path $stage 'bin\CaidoBridge.exe'
    $reported = (& $binary version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $reported -ne "CaidoBridge v$Version") {
        throw "Built binary reported an unexpected version: $reported"
    }

    foreach ($name in @(
        'install.ps1', 'doctor.ps1', 'status.ps1', 'enable-replay.ps1',
        'disable-replay.ps1', 'update-caido-token.ps1', 'uninstall.ps1',
        'LICENSE', 'THIRD_PARTY_NOTICES.md', 'THIRD_PARTY_LICENSES.txt',
        'CHANGELOG.md'
    )) {
        Copy-Item -LiteralPath (Join-Path $repositoryRoot $name) -Destination $stage
    }
    Copy-Item -LiteralPath (Join-Path $repositoryRoot 'RELEASE-README.md') `
        -Destination (Join-Path $stage 'README.md')
    foreach ($name in @(
        'autostart.ps1', 'autostart-status.ps1', 'CaidoBridge.Common.ps1',
        'doctor-runtime.ps1', 'set-replay.ps1', 'token-store.ps1',
        'update-caido-token-runtime.ps1'
    )) {
        Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) `
            -Destination (Join-Path $stage 'scripts')
    }

    $manifestLines = @()
    foreach ($file in Get-ChildItem -LiteralPath $stage -Recurse -Force -File | Sort-Object FullName) {
        $relative = $file.FullName.Substring($stage.Length + 1).Replace('\', '/')
        $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        $manifestLines += "$hash  $relative"
    }
    [IO.File]::WriteAllLines((Join-Path $stage 'SHA256SUMS.txt'), $manifestLines, [Text.UTF8Encoding]::new($false))

    Compress-Archive -LiteralPath $stage -DestinationPath $zipPath -CompressionLevel Optimal
    Expand-Archive -LiteralPath $zipPath -DestinationPath $verify
    $verifiedRoot = Join-Path $verify $archiveStem
    Assert-CBReleaseManifest -ReleaseRoot $verifiedRoot

    $releaseFiles = @(Get-ChildItem -LiteralPath $verifiedRoot -Recurse -Force -File)
    foreach ($file in $releaseFiles) {
        $relative = $file.FullName.Substring($verifiedRoot.Length + 1).Replace('\', '/')
        if ($relative -match '(^|[\\/])(cmd|internal|source)[\\/]' -or
            $relative -ieq 'bin/tunnel-client.exe' -or
            $relative -match '(?i)(config\.json|\.env|\.log$|\.dmp$)') {
            throw "Forbidden release member: $relative"
        }
    }

    $exeHash = (Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()
    $zipHash = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    [IO.File]::WriteAllText($exeSidecar, "$exeHash  CaidoBridge.exe`n", [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText($zipSidecar, "$zipHash  $($archiveStem).zip`n", [Text.UTF8Encoding]::new($false))

    Write-Host "Release archive: $zipPath"
    Write-Host "Release files: $($releaseFiles.Count)"
    Write-Host "Executable SHA-256: $exeHash"
    Write-Host "ZIP SHA-256: $zipHash"
} finally {
    Remove-Item -LiteralPath $temporaryRoot -Recurse -Force -ErrorAction SilentlyContinue
}
