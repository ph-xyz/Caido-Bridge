# Troubleshooting

Start with:

```powershell
.\doctor.ps1
.\status.ps1
```

Installed state and the bounded supervisor log are under:

```text
%LOCALAPPDATA%\PHCaidoMCP\
%LOCALAPPDATA%\PHCaidoMCP\autostart.log
```

The tunnel-client local operator UI is normally
`http://127.0.0.1:8788/ui`; readiness is `/readyz`. These endpoints are
loopback-only by default.

## Caido is closed or not ready

**Symptom:** installer says Caido must be ready, status shows waiting, or the
task runs without tunnel readiness.

Open Caido, sign in, and select the intended project. Confirm the configured
port, then rerun `doctor.ps1`. The supervisor waits rather than repeatedly
starting a server against an unavailable API.

## Incorrect CAIDO_URL

Only an absolute HTTP(S) loopback origin is accepted. Paths such as `/graphql`,
remote hostnames, credentials, queries, and fragments are rejected.

Rerun installation with the correct local port:

```powershell
.\install.ps1 -CaidoUrl http://127.0.0.1:<PORT>
```

## Expired or invalid Caido token

**Symptom:** authenticated read or project doctor fails.

Obtain a new access token using the
[official Caido GraphQL instructions](https://docs.caido.io/app/concepts/graphql.html),
then run:

```powershell
.\update-caido-token.ps1
```

Input is hidden, the new token is tested before storage, and the protected
Replay setting is preserved. `-FromClipboard` is available when needed and
clears the clipboard immediately.

## tunnel-client is absent

The installer stops before changing the system. Download the Windows amd64
archive from [OpenAI's official latest release](https://github.com/openai/tunnel-client/releases/latest),
verify its checksum, and place `tunnel-client.exe` in `bin` beside
`CaidoBridge.exe`. Do not obtain it from third-party mirrors.

## Runtime API key is absent or invalid

Create a Restricted Runtime API key in
[Platform Runtime API keys](https://platform.openai.com/settings/organization/api-keys)
with Tunnels Read + Use. Do not substitute an admin API key. Rerun the
installer; it accepts the key through hidden input and never on the command
line.

## Invalid tunnel ID

The expected format is `tunnel_` followed by 32 lowercase hexadecimal
characters. Obtain the ID from
[Platform tunnel settings](https://platform.openai.com/settings/organization/tunnels).
The tunnel ID is not the Runtime API key.

## Tunnel is not associated with the workspace

A tunnel can exist in the Platform organization without appearing in the
target ChatGPT workspace. Edit its associations in Platform tunnel settings so
the intended ChatGPT workspace is included. The daemon and ChatGPT connection
must use the same tunnel ID.

## Missing Tunnels Read + Use

Running tunnel-client and selecting a tunnel for a ChatGPT app require Tunnels
Read + Use at the organization level. Ask an organization owner or RBAC admin
to assign the appropriate role/group. OpenAI documents that new role
assignments can take time to propagate; rerun tunnel doctor afterward.

## Developer mode is unavailable

Developer mode availability depends on account, plan, and workspace policy.
For Enterprise/Edu, a workspace admin may need to grant access. When available,
enable it under **Settings → Security and login**. Secure MCP Tunnel does not
bypass this policy or turn the project into a marketplace plugin.

## Scheduled task is stopped

Run `status.ps1`, then inspect the legacy task:

```powershell
Get-ScheduledTask -TaskName 'PH Caido MCP Tunnel'
Start-ScheduledTask -TaskName 'PH Caido MCP Tunnel'
```

If it stops again, inspect `autostart.log`, confirm Caido is ready, and run
`doctor.ps1`. Rerunning `install.ps1` repairs the task idempotently.

## Health port is occupied

**Symptom:** tunnel-client cannot bind `127.0.0.1:8788`.

Identify the local owner without terminating unrelated processes:

```powershell
Get-NetTCPConnection -LocalPort 8788 -ErrorAction SilentlyContinue
```

If it belongs to another service, rerun installation with an unused local port:

```powershell
.\install.ps1 -HealthPort <PORT>
```

## Old schema after an update

Restart/reconnect the ChatGPT connection after upgrading. Open the connection
in ChatGPT Plugins, select **Refresh**, review the metadata, and start a new
conversation. Tool schemas are discovered from the currently running MCP
process, not from the ZIP on disk.

## Replay tools are missing

This is expected when Active Replay is disabled. Run:

```powershell
.\enable-replay.ps1
```

Then refresh/reconnect the developer-mode app. If they should remain absent,
run `disable-replay.ps1`. Never edit DPAPI config by hand.

## Tunnel is launched but not ready

Open `/readyz` and `/ui`, run `doctor.ps1`, and confirm:

- the Runtime API key is valid;
- the tunnel ID exists and is associated correctly;
- the runtime principal has Read + Use;
- CaidoBridge can start and authenticate to Caido;
- the health port is free;
- outbound HTTPS to OpenAI is allowed.

The tunnel needs outbound HTTPS but no inbound internet access. If a corporate
proxy, custom CA, or mTLS is required, follow the current
[Secure MCP Tunnel documentation](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels)
and tunnel-client help rather than inventing flags.

## Safe support data

Share version output, doctor/status messages, redacted log lines, and the exact
failing step. Never share config.json, DPAPI blobs, environment dumps, raw
request bodies, tokens, keys, tunnel IDs, or private target data.
