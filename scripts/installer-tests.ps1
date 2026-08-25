[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'CaidoBridge.Common.ps1')

$script:Passed = 0
$script:Failed = 0

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Invoke-Test {
    param([string]$Name, [scriptblock]$Body)
    try {
        & $Body
        $script:Passed++
        Write-Host "[PASS] $Name"
    } catch {
        $script:Failed++
        Write-Host "[FAIL] $Name - $($_.Exception.Message)"
    }
}

Invoke-Test 'loopback URL validation' {
    foreach ($url in @(
        'http://127.0.0.1:8080',
        'https://localhost:8443',
        'http://127.12.34.56',
        'http://[::1]:8080'
    )) {
        Assert-True (Test-CBLoopbackUrl -Url $url) "Expected loopback URL: $url"
    }
    foreach ($url in @(
        'https://example.com',
        'http://localhost:8080/graphql',
        'http://user@localhost:8080',
        'file:///tmp/caido'
    )) {
        Assert-True (-not (Test-CBLoopbackUrl -Url $url)) "Expected rejection: $url"
    }
}

Invoke-Test 'tunnel ID and profile validation' {
    Assert-True (Test-CBTunnelId 'tunnel_0123456789abcdef0123456789abcdef') 'Valid tunnel ID rejected.'
    Assert-True (-not (Test-CBTunnelId 'tunnel_NOT_A_REAL_ID')) 'Invalid tunnel ID accepted.'
    Assert-True (Test-CBProfileName 'caidobridge') 'CaidoBridge profile rejected.'
    Assert-True (Test-CBProfileName 'ph-caido-mcp') 'Legacy profile rejected.'
    Assert-True (-not (Test-CBProfileName '..\profile')) 'Unsafe profile accepted.'
}

Invoke-Test 'public executable identity is CaidoBridge' {
    $main = Get-Content -LiteralPath (Join-Path $repositoryRoot 'cmd\caidobridge\main.go') -Raw
    $installer = Get-Content -LiteralPath (Join-Path $repositoryRoot 'install.ps1') -Raw
    $builder = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'build-release.ps1') -Raw
    foreach ($required in @(
        'CaidoBridge v%s',
        'Name: "CaidoBridge"',
        'bin\CaidoBridge.exe',
        'CaidoBridge.exe.sha256',
        '.\cmd\caidobridge'
    )) {
        Assert-True (($main + $installer + $builder).Contains($required)) `
            "Missing public identity contract: $required"
    }
}

Invoke-Test 'v0.3.1 replay migration preserves explicit state' {
    $enabled = '{"schema_version":1,"replay_enabled":true}' | ConvertFrom-Json
    $disabled = '{"schema_version":1,"replay_enabled":false}' | ConvertFrom-Json
    $legacy = '{"schema_version":1}' | ConvertFrom-Json
    Assert-True (Get-CBReplayEnabled $enabled) 'Enabled state was not preserved.'
    Assert-True (-not (Get-CBReplayEnabled $disabled)) 'Disabled state changed.'
    Assert-True (-not (Get-CBReplayEnabled $legacy)) 'Missing legacy state must default to disabled.'
}

Invoke-Test 'tunnel-client discovery precedence' {
    $temp = Join-Path ([IO.Path]::GetTempPath()) ('caidobridge-test-' + [Guid]::NewGuid().ToString('N'))
    try {
        New-Item -ItemType Directory -Path (Join-Path $temp 'bin') -Force | Out-Null
        $requested = Join-Path $temp 'requested.exe'
        $bundled = Join-Path $temp 'bin\tunnel-client.exe'
        Set-Content -LiteralPath $requested -Value 'requested'
        Set-Content -LiteralPath $bundled -Value 'bundled'
        $resolved = Resolve-CBTunnelClient -RequestedPath $requested -ReleaseRoot $temp
        Assert-True ([IO.Path]::GetFullPath($resolved) -eq [IO.Path]::GetFullPath($requested)) `
            'Explicit path did not take precedence.'
        Remove-Item -LiteralPath $requested -Force
        $resolved = Resolve-CBTunnelClient -ReleaseRoot $temp
        Assert-True ([IO.Path]::GetFullPath($resolved) -eq [IO.Path]::GetFullPath($bundled)) `
            'bin\tunnel-client.exe was not selected.'
    } finally {
        Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Invoke-Test 'manifest validates project files and permits external tunnel-client' {
    $temp = Join-Path ([IO.Path]::GetTempPath()) ('caidobridge-manifest-' + [Guid]::NewGuid().ToString('N'))
    try {
        New-Item -ItemType Directory -Path (Join-Path $temp 'bin') -Force | Out-Null
        $projectFile = Join-Path $temp 'install.ps1'
        Set-Content -LiteralPath $projectFile -Value 'test'
        Set-Content -LiteralPath (Join-Path $temp 'bin\tunnel-client.exe') -Value 'external'
        $hash = (Get-FileHash -LiteralPath $projectFile -Algorithm SHA256).Hash.ToLowerInvariant()
        Set-Content -LiteralPath (Join-Path $temp 'SHA256SUMS.txt') -Value "$hash  install.ps1"
        Assert-CBReleaseManifest -ReleaseRoot $temp
        Set-Content -LiteralPath $projectFile -Value 'tampered'
        $threw = $false
        try { Assert-CBReleaseManifest -ReleaseRoot $temp } catch { $threw = $true }
        Assert-True $threw 'Tampered release file was accepted.'
    } finally {
        Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Invoke-Test 'preflight precedes all installation mutations' {
    $source = Get-Content -LiteralPath (Join-Path $repositoryRoot 'install.ps1') -Raw
    $doctor = $source.IndexOf('& $sourceServer doctor', [StringComparison]::Ordinal)
    $transaction = $source.IndexOf('# Transaction begins only after', [StringComparison]::Ordinal)
    $stateCreation = $source.IndexOf('New-Item -ItemType Directory -Force -Path $stateDirectory', [StringComparison]::Ordinal)
    Assert-True ($doctor -ge 0 -and $transaction -gt $doctor -and $stateCreation -gt $transaction) `
        'Installation mutation was found before the source doctor/preflight boundary.'
    $missingTunnel = $source.IndexOf('tunnel-client.exe was not found. No system changes were made.', [StringComparison]::Ordinal)
    Assert-True ($missingTunnel -ge 0 -and $missingTunnel -lt $transaction) `
        'Missing tunnel-client failure is not preflight-only.'
}

Invoke-Test 'rollback and idempotent upgrade invariants are present' {
    $source = Get-Content -LiteralPath (Join-Path $repositoryRoot 'install.ps1') -Raw
    foreach ($required in @(
        'runtime.backup.',
        'Installation failed and the previous state was restored',
        'Copy-Item -LiteralPath $profileBackup -Destination $profilePath -Force',
        'Copy-Item -LiteralPath $configBackup -Destination $configPath -Force',
        'Get-CBReplayEnabled -Configuration $loaded'
    )) {
        Assert-True ($source.Contains($required)) "Missing transaction invariant: $required"
    }
}

Invoke-Test 'uninstall exposes three confirmed scopes' {
    $source = Get-Content -LiteralPath (Join-Path $repositoryRoot 'uninstall.ps1') -Raw
    foreach ($required in @(
        "ValidateSet('Autostart', 'Runtime', 'All')",
        "SupportsShouldProcess = `$true",
        "Mode -in @('Runtime', 'All')",
        "Mode -eq 'All'"
    )) {
        Assert-True ($source.Contains($required)) "Missing uninstall invariant: $required"
    }
}

Invoke-Test 'secrets are never passed as native command arguments' {
    $files = @(
        (Join-Path $repositoryRoot 'install.ps1'),
        (Join-Path $PSScriptRoot 'autostart.ps1'),
        (Join-Path $PSScriptRoot 'doctor-runtime.ps1')
    )
    foreach ($file in $files) {
        foreach ($line in Get-Content -LiteralPath $file) {
            if ($line -match '^\s*&\s+' -and $line -match '(caidoToken|runtimeKey|CONTROL_PLANE_API_KEY|CAIDO_ACCESS_TOKEN)') {
                throw "Secret-like value found in a native command line: $file"
            }
        }
    }
}

Write-Host ''
Write-Host "Installer tests: $script:Passed passed, $script:Failed failed"
if ($script:Failed -ne 0) { exit 1 }
