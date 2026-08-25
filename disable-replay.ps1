[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$runtimeScript = Join-Path $env:LOCALAPPDATA 'PHCaidoMCP\runtime\set-replay.ps1'
if (-not (Test-Path -LiteralPath $runtimeScript -PathType Leaf)) {
    throw 'CaidoBridge is not installed.'
}
& $runtimeScript -Mode Disabled
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
