[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Enabled', 'Disabled')]
    [string]$Mode,

    [ValidateNotNullOrEmpty()]
    [string]$TaskName = 'PH Caido MCP Tunnel'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'token-store.ps1')
Assert-PHWindows

$configPath = Get-PHCaidoConfigPath
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw 'Protected configuration was not found. Install or upgrade the runtime first.'
}

$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -eq $task) {
    throw "Scheduled task '$TaskName' was not found."
}

$originalConfigJson = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8
$config = $originalConfigJson | ConvertFrom-Json -ErrorAction Stop
if ([string]::IsNullOrWhiteSpace([string]$config.tunnel_profile)) {
    throw 'Protected configuration does not contain a tunnel profile.'
}
$enabled = $Mode -eq 'Enabled'
if ($null -eq $config.PSObject.Properties['replay_enabled']) {
    $config | Add-Member -NotePropertyName replay_enabled -NotePropertyValue $enabled
} else {
    $config.replay_enabled = $enabled
}

$stateDirectory = Get-PHCaidoStateDirectory
$temporaryPath = Join-Path $stateDirectory ('config.' + [Guid]::NewGuid().ToString('N') + '.tmp')
try {
    $config | ConvertTo-Json | Set-Content -LiteralPath $temporaryPath -Encoding UTF8
    Move-Item -LiteralPath $temporaryPath -Destination $configPath -Force
    Protect-PHConfigAcl -Path $configPath
} finally {
    Remove-Item -LiteralPath $temporaryPath -Force -ErrorAction SilentlyContinue
}

try {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue

    # Stop only this profile's tunnel and its exact bundled server so the new
    # process necessarily reloads the Replay flag. Do not disturb another
    # tunnel-client profile running for a different project.
    $escapedProfile = [regex]::Escape([string]$config.tunnel_profile)
    $profilePattern = '--profile\s+{0}(?:\s|$)' -f $escapedProfile
    Get-CimInstance Win32_Process -Filter "Name='tunnel-client.exe'" `
        -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match $profilePattern } |
        ForEach-Object {
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }

    $installedServerPath = Join-Path $PSScriptRoot 'CaidoBridge.exe'
    $expectedServerPath = [IO.Path]::GetFullPath($installedServerPath)
    Get-CimInstance Win32_Process -Filter "Name='CaidoBridge.exe'" `
        -ErrorAction SilentlyContinue |
        Where-Object {
            -not [string]::IsNullOrWhiteSpace([string]$_.ExecutablePath) -and
            [string]::Equals(
                [IO.Path]::GetFullPath([string]$_.ExecutablePath),
                $expectedServerPath,
                [StringComparison]::OrdinalIgnoreCase
            )
        } |
        ForEach-Object {
            Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
        }

    Start-Sleep -Seconds 2
    Start-ScheduledTask -TaskName $TaskName
} catch {
    $restartError = $_.Exception.Message
    $rollbackTemporaryPath = Join-Path $stateDirectory `
        ('config.' + [Guid]::NewGuid().ToString('N') + '.rollback.tmp')
    try {
        Set-Content `
            -LiteralPath $rollbackTemporaryPath `
            -Value $originalConfigJson `
            -Encoding UTF8
        Move-Item -LiteralPath $rollbackTemporaryPath -Destination $configPath -Force
        Protect-PHConfigAcl -Path $configPath
    } finally {
        Remove-Item -LiteralPath $rollbackTemporaryPath -Force -ErrorAction SilentlyContinue
    }
    Start-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    throw "Could not restart the task; the previous Replay setting was restored: $restartError"
}

Write-Host ('Active Replay is now ' + $Mode.ToUpperInvariant() + '.')
Write-Host 'No credential was decrypted, printed, or changed.'
Write-Host 'Reconnect the ChatGPT developer-mode app to refresh the registered tool list.'
