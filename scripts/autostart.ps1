[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'token-store.ps1')

function Write-PHAutostartLog {
    param([Parameter(Mandatory = $true)][string]$Message)

    $stateDirectory = Get-PHCaidoStateDirectory
    New-Item -ItemType Directory -Force -Path $stateDirectory | Out-Null
    $logPath = Join-Path $stateDirectory 'autostart.log'
    if ((Test-Path -LiteralPath $logPath -PathType Leaf) -and
        (Get-Item -LiteralPath $logPath).Length -gt 262144) {
        Move-Item -LiteralPath $logPath -Destination ($logPath + '.previous') -Force
    }
    $line = '{0:o} {1}' -f [DateTime]::Now, $Message
    Add-Content -LiteralPath $logPath -Value $line -Encoding UTF8
}

function Test-PHCaidoReady {
    param([Parameter(Mandatory = $true)][string]$CaidoUrl)

    try {
        $response = Invoke-WebRequest `
            -Uri ($CaidoUrl.TrimEnd('/') + '/health') `
            -UseBasicParsing `
            -TimeoutSec 2
        if ($response.StatusCode -ne 200) {
            return $false
        }
        $health = $response.Content | ConvertFrom-Json -ErrorAction Stop
        return $health.ready -eq $true
    } catch {
        return $false
    }
}

function Test-PHTunnelReady {
    param([Parameter(Mandatory = $true)][string]$HealthListenAddress)

    try {
        $response = Invoke-WebRequest `
            -Uri ('http://' + $HealthListenAddress + '/readyz') `
            -UseBasicParsing `
            -TimeoutSec 2
        return $response.StatusCode -eq 200
    } catch {
        return $false
    }
}

try {
    $config = Import-PHCaidoConfiguration
    $tunnelClientPath = [string]$config.tunnel_client_path
    if (-not (Test-Path -LiteralPath $tunnelClientPath -PathType Leaf)) {
        throw 'tunnel-client is missing from the installed runtime.'
    }

    $replayState = if ($env:CAIDO_ENABLE_REPLAY -eq '1') { 'enabled' } else { 'disabled' }
    Write-PHAutostartLog ("supervisor started; credentials loaded from Windows DPAPI; Replay $replayState")
    $waitingForCaidoWasLogged = $false
    $existingTunnelWasLogged = $false

    while ($true) {
        if (-not (Test-PHCaidoReady -CaidoUrl ([string]$config.caido_url))) {
            if (-not $waitingForCaidoWasLogged) {
                Write-PHAutostartLog 'waiting for local Caido readiness'
                $waitingForCaidoWasLogged = $true
            }
            Start-Sleep -Seconds 5
            continue
        }
        if ($waitingForCaidoWasLogged) {
            Write-PHAutostartLog 'local Caido is ready'
            $waitingForCaidoWasLogged = $false
        }

        if (Test-PHTunnelReady -HealthListenAddress ([string]$config.health_listen_address)) {
            if (-not $existingTunnelWasLogged) {
                Write-PHAutostartLog 'tunnel is already ready; supervisor is standing by'
                $existingTunnelWasLogged = $true
            }
            Start-Sleep -Seconds 10
            continue
        }

        $existingTunnelWasLogged = $false
        Write-PHAutostartLog 'starting tunnel-client'
        # Windows PowerShell 5.1 converts a native process's stderr lines into
        # ErrorRecord objects. With the supervisor's normal Stop preference,
        # even a harmless child banner can become a terminating exception.
        # Temporarily use Continue while discarding both streams, then rely on
        # the native exit code for lifecycle handling.
        $savedErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            & $tunnelClientPath run `
                --profile ([string]$config.tunnel_profile) `
                --health.listen-addr ([string]$config.health_listen_address) `
                1> $null 2> $null
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $savedErrorActionPreference
        }
        Write-PHAutostartLog ('tunnel-client exited with code {0}; retrying' -f $exitCode)
        Start-Sleep -Seconds 10
    }
} catch {
    # This log contains only the local operational error. Secret values are
    # never interpolated or forwarded from child-process output.
    Write-PHAutostartLog ('supervisor stopped: {0}' -f $_.Exception.Message)
    exit 1
}
