[CmdletBinding()]
param([string]$RepositoryRoot)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($RepositoryRoot)) {
    $RepositoryRoot = Split-Path -Parent $PSScriptRoot
}
$root = (Resolve-Path -LiteralPath $RepositoryRoot).Path
$files = @(Get-ChildItem -LiteralPath $root -Recurse -Force -File | Where-Object {
    $_.FullName -notmatch '[\\/]\.git[\\/]' -and
    $_.FullName -notmatch '[\\/]dist[\\/]'
})

$forbiddenNames = @('.env', 'config.json')
$forbiddenExtensions = @('.exe', '.zip', '.log', '.dmp')
$violations = @()
foreach ($file in $files) {
    if ($file.Name -in $forbiddenNames -or $file.Extension.ToLowerInvariant() -in $forbiddenExtensions) {
        $violations += $file.FullName.Substring($root.Length + 1)
    }
}
if ($violations.Count -gt 0) {
    throw "Forbidden local artifacts found:`n$($violations -join "`n")"
}

$textExtensions = @('.go', '.mod', '.sum', '.md', '.ps1', '.yml', '.yaml', '.txt', '.gitignore', '.gitattributes')
$secretPatterns = @(
    '-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
    '\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b',
    '\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}\b',
    '\bgh[pousr]_[A-Za-z0-9]{36,}\b',
    '\bgithub_pat_[A-Za-z0-9_]{20,}\b',
    '\bAKIA[0-9A-Z]{16}\b',
    '\bxox[baprs]-[A-Za-z0-9-]{10,}\b',
    '\bAIza[0-9A-Za-z_-]{35}\b',
    '\bya29\.[0-9A-Za-z_-]{20,}\b',
    '\bnpm_[A-Za-z0-9]{36}\b',
    '\bpypi-[A-Za-z0-9_-]{50,}\b',
    '(?i)https?://[^\s/@:]+:[^\s/@]+@',
    '(?i)C:\\Users\\[^\\\s]+'
)
foreach ($file in $files | Where-Object { $_.Extension.ToLowerInvariant() -in $textExtensions }) {
    $content = Get-Content -LiteralPath $file.FullName -Raw -ErrorAction SilentlyContinue
    # This literal is a deliberately invalid loopback URL used to test that
    # embedded URL credentials are rejected; it is not a real credential.
    $scanContent = $content.Replace('http://user:pass@localhost:8080', '')
    $scanContent = $scanContent.Replace('https://user:pass@example.com', '')
    foreach ($pattern in $secretPatterns) {
        if ($scanContent -match $pattern) {
            throw "Potential secret or personal path found in $($file.FullName.Substring($root.Length + 1))"
        }
    }
    foreach ($match in [regex]::Matches($content, '\btunnel_[0-9a-f]{32}\b')) {
        if ($match.Value -ne 'tunnel_0123456789abcdef0123456789abcdef') {
            throw "Potential real tunnel ID found in $($file.FullName.Substring($root.Length + 1))"
        }
    }
}

Write-Host "Repository hygiene check passed for $($files.Count) files."
