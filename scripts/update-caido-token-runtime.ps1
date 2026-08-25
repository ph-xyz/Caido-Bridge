[CmdletBinding()]
param(
    [switch]$FromClipboard,

    [ValidateNotNullOrEmpty()]
    [string]$TaskName = 'PH Caido MCP Tunnel'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'CaidoBridge.Common.ps1')
. (Join-Path $PSScriptRoot 'token-store.ps1')
$config = Import-PHCaidoConfiguration

if ($FromClipboard) {
    $newToken = (Get-Clipboard -Raw).Trim()
    Set-Clipboard -Value ' '
} else {
    $newToken = Read-CBHiddenSecret -Prompt 'New Caido access token (input hidden)'
}
if ([string]::IsNullOrWhiteSpace($newToken)) {
    throw 'The new Caido access token is empty.'
}

$previousToken = $env:CAIDO_ACCESS_TOKEN
try {
    $env:CAIDO_ACCESS_TOKEN = $newToken
    & (Join-Path $PSScriptRoot 'CaidoBridge.exe') doctor
    if ($LASTEXITCODE -ne 0) {
        throw 'The new token failed the authenticated read check. Nothing was changed.'
    }

    # Save-PHCaidoConfiguration preserves replay_enabled when the parameter is
    # omitted. This is a deliberate v0.4.0 regression guard.
    Save-PHCaidoConfiguration `
        -CaidoUrl ([string]$config.caido_url) `
        -CaidoAccessToken $newToken `
        -ControlPlaneApiKey $env:CONTROL_PLANE_API_KEY `
        -TunnelClientPath ([string]$config.tunnel_client_path) `
        -Profile ([string]$config.tunnel_profile) `
        -HealthListenAddress ([string]$config.health_listen_address)

    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($null -ne $task) {
        Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        Start-ScheduledTask -TaskName $TaskName
    }
    Write-Host 'Caido token updated and DPAPI-protected. Active Replay state was preserved.'
} finally {
    $env:CAIDO_ACCESS_TOKEN = $previousToken
    Remove-Variable newToken -ErrorAction SilentlyContinue
}
