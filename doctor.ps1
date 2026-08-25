[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$runtimeScript = Join-Path $env:LOCALAPPDATA 'PHCaidoMCP\runtime\doctor-runtime.ps1'
if (-not (Test-Path -LiteralPath $runtimeScript -PathType Leaf)) {
    throw 'CaidoBridge is not installed. Run .\install.ps1 from the release directory.'
}
& $runtimeScript
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
