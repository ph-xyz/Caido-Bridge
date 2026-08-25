[CmdletBinding()]
param([switch]$FromClipboard)

$ErrorActionPreference = 'Stop'
$runtimeScript = Join-Path $env:LOCALAPPDATA 'PHCaidoMCP\runtime\update-caido-token-runtime.ps1'
if (-not (Test-Path -LiteralPath $runtimeScript -PathType Leaf)) {
    throw 'CaidoBridge is not installed.'
}
& $runtimeScript -FromClipboard:$FromClipboard
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
