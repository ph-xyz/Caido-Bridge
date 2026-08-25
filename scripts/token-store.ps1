Set-StrictMode -Version Latest

function Assert-PHWindows {
    if ($env:OS -ne 'Windows_NT') {
        throw 'This operation requires Windows DPAPI.'
    }
}

function Get-PHCaidoStateDirectory {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        throw 'LOCALAPPDATA is required to locate PH Caido MCP state.'
    }
    return Join-Path $env:LOCALAPPDATA 'PHCaidoMCP'
}

function Get-PHCaidoConfigPath {
    return Join-Path (Get-PHCaidoStateDirectory) 'config.json'
}

function Protect-PHSecret {
    param([Parameter(Mandatory = $true)][string]$PlainText)

    Assert-PHWindows
    if ([string]::IsNullOrWhiteSpace($PlainText)) {
        throw 'Cannot protect an empty secret.'
    }

    $secure = ConvertTo-SecureString -String $PlainText -AsPlainText -Force
    try {
        # Without -Key, Windows PowerShell uses DPAPI for the current user.
        return ConvertFrom-SecureString -SecureString $secure
    } finally {
        Remove-Variable secure -ErrorAction SilentlyContinue
    }
}

function Unprotect-PHSecret {
    param([Parameter(Mandatory = $true)][string]$CipherText)

    Assert-PHWindows
    $secure = ConvertTo-SecureString -String $CipherText -ErrorAction Stop
    $bstr = [IntPtr]::Zero
    try {
        $bstr = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
        return [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($bstr)
    } finally {
        if ($bstr -ne [IntPtr]::Zero) {
            [System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($bstr)
        }
        Remove-Variable secure, bstr -ErrorAction SilentlyContinue
    }
}

function Protect-PHConfigAcl {
    param([Parameter(Mandatory = $true)][string]$Path)

    Assert-PHWindows
    $currentSid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User
    $systemSid = [System.Security.Principal.SecurityIdentifier]::new(
        [System.Security.Principal.WellKnownSidType]::LocalSystemSid,
        $null
    )
    $acl = [System.Security.AccessControl.FileSecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($sid in @($currentSid, $systemSid)) {
        $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
            $sid,
            [System.Security.AccessControl.FileSystemRights]::FullControl,
            [System.Security.AccessControl.AccessControlType]::Allow
        )
        [void]$acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
}

function Save-PHCaidoConfiguration {
    param(
        [Parameter(Mandatory = $true)][string]$CaidoUrl,
        [Parameter(Mandatory = $true)][string]$CaidoAccessToken,
        [Parameter(Mandatory = $true)][string]$ControlPlaneApiKey,
        [Parameter(Mandatory = $true)][string]$TunnelClientPath,
        [Parameter(Mandatory = $true)][string]$Profile,
        [Parameter(Mandatory = $true)][string]$HealthListenAddress,
        [bool]$ReplayEnabled = $false
    )

    Assert-PHWindows
    $stateDirectory = Get-PHCaidoStateDirectory
    $configPath = Get-PHCaidoConfigPath
    New-Item -ItemType Directory -Force -Path $stateDirectory | Out-Null

    # Callers that update only a credential must not silently disable an
    # existing Active Replay opt-in. Preserve the stored value unless the
    # caller explicitly supplied -ReplayEnabled.
    $resolvedReplayEnabled = $ReplayEnabled
    if (-not $PSBoundParameters.ContainsKey('ReplayEnabled') -and
        (Test-Path -LiteralPath $configPath -PathType Leaf)) {
        try {
            $previous = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 |
                ConvertFrom-Json -ErrorAction Stop
            if ($null -ne $previous.PSObject.Properties['replay_enabled']) {
                $resolvedReplayEnabled = $previous.replay_enabled -eq $true
            }
        } finally {
            Remove-Variable previous -ErrorAction SilentlyContinue
        }
    }

    $temporaryPath = Join-Path $stateDirectory ('config.' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        $config = [ordered]@{
            schema_version                = 1
            caido_url                    = $CaidoUrl.TrimEnd('/')
            caido_access_token_dpapi     = Protect-PHSecret -PlainText $CaidoAccessToken
            control_plane_api_key_dpapi  = Protect-PHSecret -PlainText $ControlPlaneApiKey
            tunnel_client_path           = $TunnelClientPath
            tunnel_profile               = $Profile
            health_listen_address        = $HealthListenAddress
            replay_enabled               = $resolvedReplayEnabled
        }
        $config | ConvertTo-Json | Set-Content -LiteralPath $temporaryPath -Encoding UTF8
        Move-Item -LiteralPath $temporaryPath -Destination $configPath -Force
        Protect-PHConfigAcl -Path $configPath
    } finally {
        if (Test-Path -LiteralPath $temporaryPath -PathType Leaf) {
            Remove-Item -LiteralPath $temporaryPath -Force
        }
        Remove-Variable config -ErrorAction SilentlyContinue
    }
}

function Import-PHCaidoConfiguration {
    Assert-PHWindows
    $configPath = Get-PHCaidoConfigPath
    if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
        throw 'Protected configuration was not found. Run .\scripts\install-autostart.ps1 once.'
    }

    try {
        $config = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8 |
            ConvertFrom-Json -ErrorAction Stop
        if ($config.schema_version -ne 1) {
            throw 'unsupported configuration version.'
        }
        foreach ($field in @(
            'caido_url',
            'caido_access_token_dpapi',
            'control_plane_api_key_dpapi',
            'tunnel_client_path',
            'tunnel_profile',
            'health_listen_address'
        )) {
            if ([string]::IsNullOrWhiteSpace([string]$config.$field)) {
                throw "$field is missing."
            }
        }

        $caidoToken = Unprotect-PHSecret -CipherText $config.caido_access_token_dpapi
        $runtimeKey = Unprotect-PHSecret -CipherText $config.control_plane_api_key_dpapi
        if ([string]::IsNullOrWhiteSpace($caidoToken) -or
            [string]::IsNullOrWhiteSpace($runtimeKey)) {
            throw 'decrypted credential is empty.'
        }

        $env:CAIDO_URL = [string]$config.caido_url
        $env:CAIDO_ACCESS_TOKEN = $caidoToken
        $env:CONTROL_PLANE_API_KEY = $runtimeKey
        if ($null -ne $config.PSObject.Properties['replay_enabled'] -and
            $config.replay_enabled -eq $true) {
            $env:CAIDO_ENABLE_REPLAY = '1'
        } else {
            Remove-Item Env:CAIDO_ENABLE_REPLAY -ErrorAction SilentlyContinue
        }
        return $config
    } catch {
        throw "Unable to load the DPAPI-protected configuration for this Windows user: $($_.Exception.Message)"
    } finally {
        Remove-Variable caidoToken, runtimeKey -ErrorAction SilentlyContinue
    }
}
