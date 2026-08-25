[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'token-store.ps1')
$config = Import-PHCaidoConfiguration
$serverPath = Join-Path $PSScriptRoot 'CaidoBridge.exe'

& $serverPath doctor
if ($LASTEXITCODE -ne 0) {
    throw 'CaidoBridge doctor failed.'
}
& ([string]$config.tunnel_client_path) doctor `
    --profile ([string]$config.tunnel_profile) `
    --health.listen-addr '127.0.0.1:0' `
    --explain
if ($LASTEXITCODE -ne 0) {
    throw 'tunnel-client doctor failed.'
}
