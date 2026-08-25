# CaidoBridge v0.4.0 — Windows amd64

CaidoBridge connects a local Caido instance to a ChatGPT developer-mode app
through OpenAI Secure MCP Tunnel. Active Replay is disabled by default.

Product and executable name: **CaidoBridge / CaidoBridge.exe**

The v0.3.1 state path, tunnel profile, and scheduled task remain in place only
to support safe in-place upgrades.

No Caido plugin is required. CaidoBridge communicates with Caido through its
local API.

## Before installation

1. Install/open Caido 0.57.0 or newer, sign in, and select a project.
2. Download the official Windows amd64 `tunnel-client` from
   https://github.com/openai/tunnel-client/releases/latest and verify its
   official checksum.
3. Place it at `bin\tunnel-client.exe`. It is external and intentionally absent
   from this ZIP.
4. Obtain a tunnel ID associated with the target ChatGPT workspace and a
   Restricted Runtime API key with Tunnels Read + Use from
   https://platform.openai.com/settings/organization/tunnels.
5. Obtain the local Caido access token using
   https://docs.caido.io/app/concepts/graphql.html.

Never put a token or key on a command line or in a file.

## Install

```powershell
Unblock-File .\install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

The installer verifies `SHA256SUMS.txt`, the executable version, Caido
loopback readiness, both credentials, tunnel profile, task, and final
readiness. It stores secrets with Windows DPAPI and restrictive ACLs. Failure
after changes begin restores the previous runtime, profile, configuration, and
task. Running it again is safe and upgrades v0.3.1 in place.

Expected:

```text
CAIDOBRIDGE v0.4.0 INSTALLED
Active Replay: disabled
```

## Check and connect

```powershell
.\doctor.ps1
.\status.ps1
```

In ChatGPT, enable Developer mode under **Settings → Security and login**.
Open https://chatgpt.com/#settings/Connectors, select **+**, choose **Tunnel**,
select/paste the same tunnel ID, and review the eight read-only/preview tools.
Make the first test read-only.

## Operations

```powershell
.\enable-replay.ps1          # explicit opt-in; refresh the ChatGPT app
.\disable-replay.ps1         # return to read-only/preview mode
.\update-caido-token.ps1     # hidden input, preserves Replay state
.\uninstall.ps1              # confirmed Autostart, Runtime, or All modes
```

Use Active Replay only on targets you own or are explicitly authorized to
test. Select one exact Caido `scopeId`, preview the exact mutation, and pass
the returned two-minute, one-use token to the active tool without changing the
request. CaidoBridge does not fuzz, crawl, brute-force, batch, or produce
automatic vulnerability verdicts.

The executable is not Authenticode-signed and Windows may show SmartScreen.
Verify the ZIP `.sha256` before extraction and the included manifest afterward.

CaidoBridge is independent and is not officially affiliated with Caido or
OpenAI. See `LICENSE`, `THIRD_PARTY_NOTICES.md`,
`THIRD_PARTY_LICENSES.txt`, and `CHANGELOG.md`.
