[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [ValidateSet('Autostart', 'Runtime', 'All')]
    [string]$Mode,

    [ValidateNotNullOrEmpty()]
    [string]$TaskName = 'PH Caido MCP Tunnel'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($Mode)) {
    Write-Host '1 - Remove only autostart (keep runtime and protected credentials)'
    Write-Host '2 - Remove autostart and runtime (keep protected credentials)'
    Write-Host '3 - Remove autostart, runtime, logs, profile, and protected credentials'
    $choice = (Read-Host 'Choose 1, 2, or 3').Trim()
    $Mode = switch ($choice) {
        '1' { 'Autostart' }
        '2' { 'Runtime' }
        '3' { 'All' }
        default { throw 'Invalid uninstall choice.' }
    }
}

$stateDirectory = Join-Path $env:LOCALAPPDATA 'PHCaidoMCP'
$runtimeDirectory = Join-Path $stateDirectory 'runtime'
$configPath = Join-Path $stateDirectory 'config.json'
$profile = 'ph-caido-mcp'
if (Test-Path -LiteralPath $configPath -PathType Leaf) {
    try {
        $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json
        if (-not [string]::IsNullOrWhiteSpace([string]$config.tunnel_profile)) {
            $profile = [string]$config.tunnel_profile
        }
    } catch {
        Write-Warning 'The protected configuration metadata could not be parsed; using the legacy profile name.'
    }
}

if ($PSCmdlet.ShouldProcess($TaskName, 'stop and remove the Windows logon task')) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
}

if ($Mode -in @('Runtime', 'All')) {
    $escapedProfile = [regex]::Escape($profile)
    $pattern = '--profile\s+{0}(?:\s|$)' -f $escapedProfile
    Get-CimInstance Win32_Process -Filter "Name='tunnel-client.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match $pattern } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }

    if ((Test-Path -LiteralPath $runtimeDirectory -PathType Container) -and
        $PSCmdlet.ShouldProcess($runtimeDirectory, 'remove the installed CaidoBridge runtime')) {
        $stateFull = [IO.Path]::GetFullPath($stateDirectory).TrimEnd('\') + '\'
        $runtimeFull = [IO.Path]::GetFullPath($runtimeDirectory)
        if (-not $runtimeFull.StartsWith($stateFull, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Refusing to remove a runtime outside the CaidoBridge state directory.'
        }
        Remove-Item -LiteralPath $runtimeFull -Recurse -Force
    }
}

if ($Mode -eq 'All') {
    $profileDirectory = [Environment]::GetEnvironmentVariable('TUNNEL_CLIENT_PROFILE_DIR', 'Process')
    if ([string]::IsNullOrWhiteSpace($profileDirectory)) {
        $profileDirectory = Join-Path $env:APPDATA 'tunnel-client'
    }
    $profilePath = Join-Path $profileDirectory ($profile + '.yaml')
    if ((Test-Path -LiteralPath $profilePath -PathType Leaf) -and
        $PSCmdlet.ShouldProcess($profilePath, 'remove the CaidoBridge tunnel profile')) {
        Remove-Item -LiteralPath $profilePath -Force
    }
    if ((Test-Path -LiteralPath $stateDirectory -PathType Container) -and
        $PSCmdlet.ShouldProcess($stateDirectory, 'remove logs and DPAPI-protected credentials')) {
        $expected = [IO.Path]::GetFullPath((Join-Path $env:LOCALAPPDATA 'PHCaidoMCP'))
        if (-not [string]::Equals([IO.Path]::GetFullPath($stateDirectory), $expected,
            [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Refusing to remove an unexpected state directory.'
        }
        Remove-Item -LiteralPath $stateDirectory -Recurse -Force
    }
}

Write-Host "CaidoBridge uninstall mode completed: $Mode"
