[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$runtimeScript = Join-Path $env:LOCALAPPDATA 'PHCaidoMCP\runtime\set-replay.ps1'
if (-not (Test-Path -LiteralPath $runtimeScript -PathType Leaf)) {
    throw 'CaidoBridge is not installed.'
}
Write-Warning 'Active Replay can send traffic to in-scope targets and may change application state.'
& $runtimeScript -Mode Enabled
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
