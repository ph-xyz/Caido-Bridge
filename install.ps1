[CmdletBinding()]
param(
    [string]$CaidoUrl = 'http://127.0.0.1:8080',

    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$Profile = 'ph-caido-mcp',

    [ValidatePattern('^tunnel_[0-9a-f]{32}$')]
    [string]$TunnelId,

    [ValidateRange(1024, 65535)]
    [int]$HealthPort = 8788,

    [string]$TunnelClientPath,

    [switch]$CaidoTokenFromClipboard,

    [ValidateNotNullOrEmpty()]
    [string]$TaskName = 'PH Caido MCP Tunnel'
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$releaseRoot = $PSScriptRoot
. (Join-Path $releaseRoot 'scripts\CaidoBridge.Common.ps1')
. (Join-Path $releaseRoot 'scripts\token-store.ps1')

function Stop-CBInstalledProcesses {
    param(
        [Parameter(Mandatory = $true)][string]$ProfileName,
        [string[]]$ServerPaths = @()
    )

    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    $escapedProfile = [regex]::Escape($ProfileName)
    $pattern = '--profile\s+{0}(?:\s|$)' -f $escapedProfile
    Get-CimInstance Win32_Process -Filter "Name='tunnel-client.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -match $pattern } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }

    $expected = @($ServerPaths | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } |
        ForEach-Object { [IO.Path]::GetFullPath($_) })
    Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
        Where-Object {
            $_.Name -in @('CaidoBridge.exe', 'ph-caido-mcp.exe') -and
            -not [string]::IsNullOrWhiteSpace([string]$_.ExecutablePath) -and
            $expected -contains [IO.Path]::GetFullPath([string]$_.ExecutablePath)
        } |
        ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 2
}

function Register-CBAutostartTask {
    param([Parameter(Mandatory = $true)][string]$AutostartPath)

    $powershellPath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $arguments = '-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "' +
        $AutostartPath + '"'
    $action = New-ScheduledTaskAction -Execute $powershellPath -Argument $arguments
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
    $principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -RestartCount 3 `
        -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) `
        -StartWhenAvailable
    $task = New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal `
        -Settings $settings -Description 'Starts the project-guarded CaidoBridge tunnel after local Caido becomes ready.'
    Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
}

function Find-CBTunnelIdInProfile {
    param([Parameter(Mandatory = $true)][string]$ProfilePath)

    if (-not (Test-Path -LiteralPath $ProfilePath -PathType Leaf)) {
        return $null
    }
    $match = [regex]::Match(
        (Get-Content -LiteralPath $ProfilePath -Raw -Encoding UTF8),
        'tunnel_[0-9a-f]{32}'
    )
    if ($match.Success) {
        return $match.Value
    }
    return $null
}

Write-Host 'CaidoBridge v0.4.0 guided installer'
Write-Host 'Legacy state, profile, and task identifiers are retained for seamless v0.3.1 migration.'
Write-Host ''

# Preflight: everything through the source doctor is read-only.
Assert-CBWindowsAmd64
Assert-CBReleaseManifest -ReleaseRoot $releaseRoot

$sourceServer = Join-Path $releaseRoot 'bin\CaidoBridge.exe'
if (-not (Test-Path -LiteralPath $sourceServer -PathType Leaf)) {
    throw 'bin\CaidoBridge.exe is missing from the release.'
}
$reportedVersion = (& $sourceServer version | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $reportedVersion -ne 'CaidoBridge v0.4.0') {
    throw "Unexpected CaidoBridge binary version: $reportedVersion"
}

$existingConfigJson = $null
$existingConfig = $null
$configPath = Get-PHCaidoConfigPath
if (Test-Path -LiteralPath $configPath -PathType Leaf) {
    $existingConfigJson = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8
    $existingConfig = $existingConfigJson | ConvertFrom-Json -ErrorAction Stop
    if (-not $PSBoundParameters.ContainsKey('CaidoUrl')) {
        $CaidoUrl = [string]$existingConfig.caido_url
    }
    if (-not $PSBoundParameters.ContainsKey('Profile')) {
        $Profile = [string]$existingConfig.tunnel_profile
    }
}

if (-not (Test-CBLoopbackUrl -Url $CaidoUrl)) {
    throw 'CAIDO_URL must be an absolute loopback-only HTTP(S) URL without path, query, fragment, or userinfo.'
}
if (-not (Test-CBProfileName -Profile $Profile)) {
    throw 'The tunnel profile name is invalid.'
}

$existingTunnelPath = if ($null -ne $existingConfig) {
    [string]$existingConfig.tunnel_client_path
} else {
    $null
}
$resolvedTunnelClient = Resolve-CBTunnelClient -RequestedPath $TunnelClientPath `
    -ReleaseRoot $releaseRoot -ExistingInstalledPath $existingTunnelPath
if ([string]::IsNullOrWhiteSpace($resolvedTunnelClient)) {
    throw @"
tunnel-client.exe was not found. No system changes were made.

Download the supported Windows amd64 release from:
https://github.com/openai/tunnel-client/releases/latest

Expected file: tunnel-client.exe
Place it at:  $releaseRoot\bin\tunnel-client.exe
Then continue with:
powershell -NoProfile -ExecutionPolicy Bypass -File "$releaseRoot\install.ps1"
"@
}
$null = & $resolvedTunnelClient --version 2>&1
if ($LASTEXITCODE -ne 0) {
    throw 'tunnel-client.exe could not report its version. Verify the official download and checksum.'
}

try {
    $healthResponse = Invoke-WebRequest -Uri ($CaidoUrl.TrimEnd('/') + '/health') `
        -UseBasicParsing -TimeoutSec 3
    $health = $healthResponse.Content | ConvertFrom-Json -ErrorAction Stop
    if ($healthResponse.StatusCode -ne 200 -or $health.ready -ne $true) {
        throw 'Caido is reachable but not ready.'
    }
} catch {
    throw "Caido must be open and ready at $CaidoUrl before installation: $($_.Exception.Message)"
}

$profilePath = Get-CBTunnelProfilePath -Profile $Profile
if (-not (Test-CBTunnelId -TunnelId $TunnelId)) {
    $TunnelId = Find-CBTunnelIdInProfile -ProfilePath $profilePath
}
if (-not (Test-CBTunnelId -TunnelId $TunnelId)) {
    $TunnelId = (Read-Host 'Tunnel ID (tunnel_ followed by 32 lowercase hex characters)').Trim()
}
if (-not (Test-CBTunnelId -TunnelId $TunnelId)) {
    throw 'Invalid tunnel ID. Obtain it from https://platform.openai.com/settings/organization/tunnels.'
}

$caidoToken = $null
$runtimeKey = $null
$replayEnabled = $false
if ($null -ne $existingConfig) {
    $loaded = Import-PHCaidoConfiguration
    $caidoToken = $env:CAIDO_ACCESS_TOKEN
    $runtimeKey = $env:CONTROL_PLANE_API_KEY
    $replayEnabled = Get-CBReplayEnabled -Configuration $loaded
}
if ([string]::IsNullOrWhiteSpace($caidoToken)) {
    if ($CaidoTokenFromClipboard) {
        $caidoToken = (Get-Clipboard -Raw).Trim()
        Set-Clipboard -Value ' '
    } else {
        Write-Host 'Obtain the local access token using the official Caido GraphQL documentation.'
        $caidoToken = Read-CBHiddenSecret -Prompt 'Caido access token (input hidden)'
    }
}
if ([string]::IsNullOrWhiteSpace($caidoToken)) {
    throw 'The Caido access token is required.'
}
if ([string]::IsNullOrWhiteSpace($runtimeKey)) {
    Write-Host 'Create a Restricted Runtime API key with Tunnels Read + Use.'
    $runtimeKey = Read-CBHiddenSecret -Prompt 'OpenAI Runtime API key (input hidden)'
}
if ([string]::IsNullOrWhiteSpace($runtimeKey)) {
    throw 'The OpenAI Runtime API key is required.'
}

$oldCaidoUrl = $env:CAIDO_URL
$oldCaidoToken = $env:CAIDO_ACCESS_TOKEN
$oldRuntimeKey = $env:CONTROL_PLANE_API_KEY
$oldReplay = $env:CAIDO_ENABLE_REPLAY
$env:CAIDO_URL = $CaidoUrl.TrimEnd('/')
$env:CAIDO_ACCESS_TOKEN = $caidoToken
$env:CONTROL_PLANE_API_KEY = $runtimeKey
if ($replayEnabled) {
    $env:CAIDO_ENABLE_REPLAY = '1'
} else {
    Remove-Item Env:CAIDO_ENABLE_REPLAY -ErrorAction SilentlyContinue
}

try {
    Write-Host '[preflight] Validating Caido authentication, project, and read-only History access...'
    & $sourceServer doctor
    if ($LASTEXITCODE -ne 0) {
        throw 'CaidoBridge doctor failed. No system changes were made.'
    }

    # Transaction begins only after all release, dependency, Caido, and secret checks pass.
    $stateDirectory = Get-PHCaidoStateDirectory
    $stateExisted = Test-Path -LiteralPath $stateDirectory -PathType Container
    New-Item -ItemType Directory -Force -Path $stateDirectory | Out-Null
    $runtimeDirectory = Join-Path $stateDirectory 'runtime'
    $timestamp = [DateTime]::UtcNow.ToString('yyyyMMdd-HHmmss')
    $stagingDirectory = Assert-CBChildPath -Parent $stateDirectory `
        -Child (Join-Path $stateDirectory ('runtime.staging.' + [Guid]::NewGuid().ToString('N')))
    $runtimeBackup = Assert-CBChildPath -Parent $stateDirectory `
        -Child (Join-Path $stateDirectory ('runtime.backup.' + $timestamp))
    $configBackup = Assert-CBChildPath -Parent $stateDirectory `
        -Child (Join-Path $stateDirectory ('config.backup.' + $timestamp + '.json'))
    $profileBackup = Join-Path ([IO.Path]::GetTempPath()) `
        ('caidobridge-profile-' + [Guid]::NewGuid().ToString('N') + '.yaml')
    $profileExisted = Test-Path -LiteralPath $profilePath -PathType Leaf
    $taskExisted = $null -ne (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue)
    $runtimeReplaced = $false
    $profileChanged = $false
    $configChanged = $false

    try {
        Write-Host '[1/6] Staging the CaidoBridge runtime...'
        New-Item -ItemType Directory -Path $stagingDirectory | Out-Null
        Copy-Item -LiteralPath $sourceServer -Destination (Join-Path $stagingDirectory 'CaidoBridge.exe')
        Copy-Item -LiteralPath $resolvedTunnelClient -Destination (Join-Path $stagingDirectory 'tunnel-client.exe')
        foreach ($scriptName in @(
            'autostart.ps1',
            'autostart-status.ps1',
            'CaidoBridge.Common.ps1',
            'doctor-runtime.ps1',
            'set-replay.ps1',
            'token-store.ps1',
            'update-caido-token-runtime.ps1'
        )) {
            Copy-Item -LiteralPath (Join-Path $releaseRoot ('scripts\' + $scriptName)) `
                -Destination (Join-Path $stagingDirectory $scriptName)
        }

        Write-Host '[2/6] Stopping only the legacy CaidoBridge task and matching processes...'
        Stop-CBInstalledProcesses -ProfileName $Profile -ServerPaths @(
            (Join-Path $runtimeDirectory 'CaidoBridge.exe'),
            (Join-Path $runtimeDirectory 'ph-caido-mcp.exe'),
            (Join-Path $stagingDirectory 'CaidoBridge.exe')
        )
        if (Test-Path -LiteralPath $runtimeDirectory -PathType Container) {
            Move-Item -LiteralPath $runtimeDirectory -Destination $runtimeBackup
        }
        Move-Item -LiteralPath $stagingDirectory -Destination $runtimeDirectory
        $runtimeReplaced = $true

        $installedServer = Join-Path $runtimeDirectory 'CaidoBridge.exe'
        $installedTunnelClient = Join-Path $runtimeDirectory 'tunnel-client.exe'
        $portableServerPath = $installedServer.Replace('\', '/')
        $mcpCommand = "'" + $portableServerPath + "' serve"

        Write-Host '[3/6] Creating and validating the tunnel-client profile...'
        if ($profileExisted) {
            Copy-Item -LiteralPath $profilePath -Destination $profileBackup
        }
        # init may write the profile before reporting a later preflight error;
        # enable restoration before invoking it.
        $profileChanged = $true
        & $installedTunnelClient init --sample sample_mcp_stdio_local --profile $Profile `
            --tunnel-id $TunnelId --mcp-command $mcpCommand `
            --health-listen-addr "127.0.0.1:$HealthPort" --force
        if ($LASTEXITCODE -ne 0) {
            throw 'tunnel-client init failed.'
        }
        & $installedTunnelClient doctor --profile $Profile `
            --health.listen-addr '127.0.0.1:0' --explain
        if ($LASTEXITCODE -ne 0) {
            throw 'tunnel-client doctor failed.'
        }

        Write-Host '[4/6] Storing credentials with Windows DPAPI and restrictive ACLs...'
        if (Test-Path -LiteralPath $configPath -PathType Leaf) {
            Copy-Item -LiteralPath $configPath -Destination $configBackup
            Protect-PHConfigAcl -Path $configBackup
        }
        # Save is atomic, but ACL application happens after replacement. Mark
        # the config changed first so either failure path restores the backup.
        $configChanged = $true
        Save-PHCaidoConfiguration -CaidoUrl $CaidoUrl -CaidoAccessToken $caidoToken `
            -ControlPlaneApiKey $runtimeKey -TunnelClientPath $installedTunnelClient `
            -Profile $Profile -HealthListenAddress "127.0.0.1:$HealthPort" `
            -ReplayEnabled $replayEnabled
        Write-Host '[5/6] Registering the limited Windows logon task...'
        Register-CBAutostartTask -AutostartPath (Join-Path $runtimeDirectory 'autostart.ps1')
        Start-ScheduledTask -TaskName $TaskName

        Write-Host '[6/6] Waiting for Secure MCP Tunnel readiness...'
        if (-not (Wait-CBTunnelReady -Address "127.0.0.1:$HealthPort" -TimeoutSeconds 45)) {
            throw 'The Secure MCP Tunnel did not become ready within 45 seconds.'
        }
        & (Join-Path $runtimeDirectory 'doctor-runtime.ps1')
        if ($LASTEXITCODE -ne 0) {
            throw 'Final doctor failed.'
        }
    } catch {
        $installError = $_.Exception.Message
        Write-Warning 'Installation validation failed. Rolling back every changed component...'
        Stop-CBInstalledProcesses -ProfileName $Profile -ServerPaths @(
            (Join-Path $runtimeDirectory 'CaidoBridge.exe'),
            (Join-Path $runtimeDirectory 'ph-caido-mcp.exe')
        )
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue
        if ($runtimeReplaced -and (Test-Path -LiteralPath $runtimeDirectory -PathType Container)) {
            Remove-Item -LiteralPath $runtimeDirectory -Recurse -Force
        }
        if (Test-Path -LiteralPath $runtimeBackup -PathType Container) {
            Move-Item -LiteralPath $runtimeBackup -Destination $runtimeDirectory
        }
        if ($profileChanged) {
            if ($profileExisted -and (Test-Path -LiteralPath $profileBackup -PathType Leaf)) {
                Copy-Item -LiteralPath $profileBackup -Destination $profilePath -Force
            } else {
                Remove-Item -LiteralPath $profilePath -Force -ErrorAction SilentlyContinue
            }
        }
        if ($configChanged) {
            if (Test-Path -LiteralPath $configBackup -PathType Leaf) {
                Copy-Item -LiteralPath $configBackup -Destination $configPath -Force
                Protect-PHConfigAcl -Path $configPath
            } else {
                Remove-Item -LiteralPath $configPath -Force -ErrorAction SilentlyContinue
            }
        }
        if ($taskExisted -and (Test-Path -LiteralPath $runtimeDirectory -PathType Container)) {
            Register-CBAutostartTask -AutostartPath (Join-Path $runtimeDirectory 'autostart.ps1')
            Start-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        }
        if (-not $stateExisted -and (Test-Path -LiteralPath $stateDirectory -PathType Container) -and
            @(Get-ChildItem -LiteralPath $stateDirectory -Force).Count -eq 0) {
            Remove-Item -LiteralPath $stateDirectory -Force
        }
        throw "Installation failed and the previous state was restored: $installError"
    } finally {
        Remove-Item -LiteralPath $stagingDirectory -Recurse -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $configBackup -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $profileBackup -Force -ErrorAction SilentlyContinue
    }

    Write-Host ''
    Write-Host 'CAIDOBRIDGE v0.4.0 INSTALLED'
    Write-Host ('Active Replay: ' + $(if ($replayEnabled) { 'ENABLED (preserved from the previous installation)' } else { 'disabled' }))
    Write-Host 'Runtime: %LOCALAPPDATA%\PHCaidoMCP\runtime (legacy state path retained for migration)'
    Write-Host 'Next: enable Developer mode in ChatGPT, create an app, choose Tunnel, and select the same tunnel ID.'
} finally {
    $env:CAIDO_URL = $oldCaidoUrl
    $env:CAIDO_ACCESS_TOKEN = $oldCaidoToken
    $env:CONTROL_PLANE_API_KEY = $oldRuntimeKey
    $env:CAIDO_ENABLE_REPLAY = $oldReplay
    Remove-Variable caidoToken, runtimeKey -ErrorAction SilentlyContinue
}
