[CmdletBinding()]
param(
    [ValidateNotNullOrEmpty()]
    [string]$TaskName = 'PH Caido MCP Tunnel'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'token-store.ps1')
$configPath = Get-PHCaidoConfigPath
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    throw 'Protected configuration was not found. Run .\scripts\install-autostart.ps1.'
}
$config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 | ConvertFrom-Json
$replayEnabled = $null -ne $config.PSObject.Properties['replay_enabled'] -and
    $config.replay_enabled -eq $true
Write-Host ('[info] Active Replay: ' + $(if ($replayEnabled) { 'ENABLED' } else { 'disabled' }))

$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -eq $task) {
    Write-Host '[fail] Windows logon task is not installed'
} else {
    Write-Host ("[ok] Windows logon task: {0}" -f $task.State)
}

try {
    $caido = Invoke-WebRequest `
        -Uri (([string]$config.caido_url).TrimEnd('/') + '/health') `
        -UseBasicParsing `
        -TimeoutSec 2
    $health = $caido.Content | ConvertFrom-Json
    if ($caido.StatusCode -eq 200 -and $health.ready -eq $true) {
        Write-Host '[ok] Local Caido is ready'
    } else {
        Write-Host '[wait] Local Caido is reachable but not ready'
    }
} catch {
    Write-Host '[wait] Local Caido is not open or not ready'
}

try {
    $tunnel = Invoke-WebRequest `
        -Uri ('http://' + [string]$config.health_listen_address + '/readyz') `
        -UseBasicParsing `
        -TimeoutSec 2
    if ($tunnel.StatusCode -eq 200) {
        Write-Host '[ok] Secure MCP Tunnel is ready'
    } else {
        Write-Host '[wait] Secure MCP Tunnel is not ready'
    }
} catch {
    Write-Host '[wait] Secure MCP Tunnel is not ready'
}

Write-Host ('Log: ' + (Join-Path (Get-PHCaidoStateDirectory) 'autostart.log'))
