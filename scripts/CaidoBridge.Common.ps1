Set-StrictMode -Version Latest

$script:CaidoBridgeVersion = '0.4.0'
$script:CaidoBridgeLegacyTaskName = 'PH Caido MCP Tunnel'
$script:CaidoBridgeLegacyProfile = 'ph-caido-mcp'

function Test-CBLoopbackUrl {
    param([Parameter(Mandatory = $true)][string]$Url)

    try {
        $uri = [Uri]$Url
    } catch {
        return $false
    }
    if (-not $uri.IsAbsoluteUri -or $uri.Scheme -notin @('http', 'https')) {
        return $false
    }
    if (-not [string]::IsNullOrEmpty($uri.UserInfo) -or
        -not [string]::IsNullOrEmpty($uri.Query) -or
        -not [string]::IsNullOrEmpty($uri.Fragment) -or
        $uri.AbsolutePath -ne '/') {
        return $false
    }
    $hostName = $uri.DnsSafeHost.TrimEnd('.').ToLowerInvariant()
    if ($hostName -eq 'localhost') {
        return $true
    }
    $ip = $null
    if (-not [Net.IPAddress]::TryParse($hostName, [ref]$ip)) {
        return $false
    }
    return [Net.IPAddress]::IsLoopback($ip)
}

function Test-CBTunnelId {
    param([AllowNull()][string]$TunnelId)

    return -not [string]::IsNullOrWhiteSpace($TunnelId) -and
        $TunnelId -match '^tunnel_[0-9a-f]{32}$'
}

function Test-CBProfileName {
    param([AllowNull()][string]$Profile)

    return -not [string]::IsNullOrWhiteSpace($Profile) -and
        $Profile -match '^[A-Za-z0-9._-]+$'
}

function Get-CBReplayEnabled {
    param([AllowNull()]$Configuration)

    return $null -ne $Configuration -and
        $null -ne $Configuration.PSObject.Properties['replay_enabled'] -and
        $Configuration.replay_enabled -eq $true
}

function Assert-CBWindowsAmd64 {
    if ($env:OS -ne 'Windows_NT') {
        throw 'CaidoBridge v0.4.0 supports Windows only.'
    }
    $architecture = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    if ($architecture -ne [Runtime.InteropServices.Architecture]::X64) {
        throw "CaidoBridge v0.4.0 requires Windows amd64; detected $architecture."
    }
}

function Assert-CBChildPath {
    param(
        [Parameter(Mandatory = $true)][string]$Parent,
        [Parameter(Mandatory = $true)][string]$Child
    )

    $parentFull = [IO.Path]::GetFullPath($Parent).TrimEnd('\') + '\'
    $childFull = [IO.Path]::GetFullPath($Child)
    if (-not $childFull.StartsWith($parentFull, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to modify a path outside the CaidoBridge state directory: $childFull"
    }
    return $childFull
}

function Get-CBTunnelProfilePath {
    param([Parameter(Mandatory = $true)][string]$Profile)

    $profileDirectory = [Environment]::GetEnvironmentVariable('TUNNEL_CLIENT_PROFILE_DIR', 'Process')
    if ([string]::IsNullOrWhiteSpace($profileDirectory)) {
        $profileDirectory = [Environment]::GetEnvironmentVariable('TUNNEL_CLIENT_PROFILE_DIR', 'User')
    }
    if ([string]::IsNullOrWhiteSpace($profileDirectory)) {
        $profileDirectory = Join-Path $env:APPDATA 'tunnel-client'
    }
    return Join-Path $profileDirectory ($Profile + '.yaml')
}

function Resolve-CBTunnelClient {
    param(
        [string]$RequestedPath,
        [Parameter(Mandatory = $true)][string]$ReleaseRoot,
        [string]$ExistingInstalledPath
    )

    $candidates = [Collections.Generic.List[string]]::new()
    if (-not [string]::IsNullOrWhiteSpace($RequestedPath)) {
        $candidates.Add($RequestedPath)
    }
    $candidates.Add((Join-Path $ReleaseRoot 'bin\tunnel-client.exe'))
    $candidates.Add((Join-Path $ReleaseRoot 'tunnel-client.exe'))
    $command = Get-Command 'tunnel-client.exe' -ErrorAction SilentlyContinue
    if ($null -eq $command) {
        $command = Get-Command 'tunnel-client' -ErrorAction SilentlyContinue
    }
    if ($null -ne $command -and -not [string]::IsNullOrWhiteSpace($command.Source)) {
        $candidates.Add($command.Source)
    }
    if (-not [string]::IsNullOrWhiteSpace($ExistingInstalledPath)) {
        $candidates.Add($ExistingInstalledPath)
    }

    foreach ($candidate in $candidates) {
        if (-not [string]::IsNullOrWhiteSpace($candidate) -and
            (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    return $null
}

function Get-CBManifestEntries {
    param([Parameter(Mandatory = $true)][string]$ManifestPath)

    $entries = @()
    foreach ($line in Get-Content -LiteralPath $ManifestPath -Encoding UTF8) {
        if ([string]::IsNullOrWhiteSpace($line)) {
            continue
        }
        $match = [regex]::Match($line, '^(?<hash>[0-9a-fA-F]{64})\s+\*?(?<path>.+)$')
        if (-not $match.Success) {
            throw "Invalid SHA256SUMS.txt line: $line"
        }
        $relative = $match.Groups['path'].Value.Replace('/', '\')
        if ([IO.Path]::IsPathRooted($relative) -or $relative.Split('\') -contains '..') {
            throw "Unsafe manifest path: $relative"
        }
        $entries += [pscustomobject]@{
            Hash = $match.Groups['hash'].Value.ToLowerInvariant()
            Path = $relative
        }
    }
    if ($entries.Count -eq 0) {
        throw 'SHA256SUMS.txt is empty.'
    }
    return $entries
}

function Assert-CBReleaseManifest {
    param([Parameter(Mandatory = $true)][string]$ReleaseRoot)

    $root = (Resolve-Path -LiteralPath $ReleaseRoot).Path
    $manifestPath = Join-Path $root 'SHA256SUMS.txt'
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw 'SHA256SUMS.txt is missing. Use the official CaidoBridge release ZIP.'
    }
    $entries = @(Get-CBManifestEntries -ManifestPath $manifestPath)
    $listed = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $entries) {
        $path = Assert-CBChildPath -Parent $root -Child (Join-Path $root $entry.Path)
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Release file is missing: $($entry.Path)"
        }
        $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $entry.Hash) {
            throw "Release file checksum mismatch: $($entry.Path)"
        }
        if (-not $listed.Add($entry.Path.Replace('\', '/'))) {
            throw "Duplicate manifest path: $($entry.Path)"
        }
    }

    foreach ($file in Get-ChildItem -LiteralPath $root -Recurse -Force -File) {
        $relative = $file.FullName.Substring($root.Length + 1).Replace('\', '/')
        if ($relative -eq 'SHA256SUMS.txt' -or
            $relative -ieq 'bin/tunnel-client.exe' -or
            $relative.EndsWith(':Zone.Identifier', [StringComparison]::OrdinalIgnoreCase)) {
            continue
        }
        if (-not $listed.Contains($relative)) {
            throw "Unexpected file in release directory: $relative"
        }
    }
}

function Convert-CBSecureStringToPlainText {
    param([Parameter(Mandatory = $true)][Security.SecureString]$SecureString)

    $bstr = [IntPtr]::Zero
    try {
        $bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureString)
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    } finally {
        if ($bstr -ne [IntPtr]::Zero) {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
        }
    }
}

function Read-CBHiddenSecret {
    param([Parameter(Mandatory = $true)][string]$Prompt)

    $secure = Read-Host $Prompt -AsSecureString
    try {
        return Convert-CBSecureStringToPlainText -SecureString $secure
    } finally {
        Remove-Variable secure -ErrorAction SilentlyContinue
    }
}

function Wait-CBTunnelReady {
    param(
        [Parameter(Mandatory = $true)][string]$Address,
        [int]$TimeoutSeconds = 45
    )

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $response = Invoke-WebRequest -Uri ('http://' + $Address + '/readyz') `
                -UseBasicParsing -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                return $true
            }
        } catch {
            # Readiness is expected to be transient while the task starts.
        }
        Start-Sleep -Seconds 2
    }
    return $false
}
