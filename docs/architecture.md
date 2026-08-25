# Architecture

## Runtime topology

```text
ChatGPT
   ↕ OpenAI-hosted tunnel endpoint
tunnel-client
   ↕ stdio
CaidoBridge
   ↕ local loopback GraphQL
Caido
```

`tunnel-client` opens an outbound HTTPS connection to the OpenAI tunnel
control plane and launches the `CaidoBridge.exe serve` command over
stdio. CaidoBridge calls only the exact validated loopback origin configured as
`CAIDO_URL`. Caido itself owns captured traffic, projects, scopes, and Replay.

No Caido plugin is required. The `caido-community/sdk-go` module is a build-time
GraphQL client library, not an installed Caido plugin.

## Trust boundaries

1. **ChatGPT/OpenAI boundary:** tool calls arrive through the authenticated
   tunnel associated with a Platform organization and ChatGPT workspace.
2. **Local tunnel boundary:** `tunnel-client` is an external OpenAI binary. It
   receives a Runtime API key and starts CaidoBridge over stdio.
3. **CaidoBridge boundary:** the MCP server validates tool schemas, project and
   request identity, scopes, mutations, confirmations, redaction, and target
   origin before calling Caido.
4. **Caido boundary:** Caido authenticates GraphQL calls and is the only
   component that sends a Replay request to the target.
5. **Target boundary:** a Replay leaves the local machine and can change target
   state. Scope and confirmation reduce accidental use; authorization remains
   the operator's responsibility.

## Secret flow

```text
Hidden installer input
  ├─ Caido access token ─┐
  └─ Runtime API key ────┴─> current-user DPAPI ciphertext in config.json
                                      │
                         runtime token-store.ps1
                                      │ process environment only
                    ┌─────────────────┴─────────────────┐
                    │                                   │
             CaidoBridge                         tunnel-client
       Authorization on exact             control-plane authentication
       loopback POST /graphql
```

The config file is protected with an ACL granting full control only to the
current Windows user and Local System. Secrets are never command-line
arguments, MCP results, normal logs, tunnel profile fields, or repository
files. Plaintext necessarily exists briefly in the installer/runtime process
memory and environment.

## Request flow

For observation tools, CaidoBridge validates the current project, reads local
GraphQL data, rechecks the project, redacts sensitive headers, bounds body
sizes, and returns factual results.

For a preview, it additionally reconstructs the raw request, verifies method,
host, path and framing, applies structured mutations to a copy, evaluates one
exact selected Caido scope, and returns both fingerprints plus a cryptographically
random, two-minute, one-use token without sending target traffic.

For Active Replay, the server repeats all checks, consumes a token bound to the
same project, History row, scope, source fingerprint, and prepared request
fingerprint, then requires the remaining confirmations, creates/updates a Caido
Replay draft, starts one task, polls its result, and returns redacted evidence
and objective diffs.

## Read-only and Active Replay boundaries

The server always registers eight observation/preview tools. Active Replay is
not an authorization hint layered over registered tools: the two active tools
do not exist in MCP discovery until the DPAPI-protected configuration contains
an explicit opt-in. Changing this flag restarts the matching tunnel profile so
the server advertises the correct surface.

Read-only operations never select a project or send a target request. Active
operations cannot change the connection origin or Host and cannot bypass the
Caido scope evaluator or state-changing confirmations.

## Local storage and logs

Legacy compatibility paths are retained:

```text
%LOCALAPPDATA%\PHCaidoMCP\
├── config.json              DPAPI ciphertext + non-secret runtime metadata
├── autostart.log            bounded operational log, no child output
└── runtime\                 installed binaries and scripts
```

`autostart.log` records readiness and lifecycle events, rotates at 256 KiB,
and deliberately discards child stdout/stderr so secret-bearing error payloads
cannot be copied into it. The tunnel-client has its own loopback-only `/ui`,
`/healthz`, `/readyz`, and `/metrics` surfaces and applies its own redaction.

## Failure modes

- If Caido is closed or not ready, the supervisor waits without starting the
  MCP child.
- If authentication or project checks fail, CaidoBridge exits and no MCP tools
  are served.
- If the tunnel is disconnected, ChatGPT calls fail until it reconnects; no
  inbound fallback is opened.
- If project selection changes during an operation, the result is discarded.
  A target request already sent by Caido cannot be undone.
- If installation fails after mutation begins, runtime, profile,
  DPAPI configuration, and scheduled task are restored.
- If a Replay response times out or is ambiguous, the tool returns a factual
  error and never labels a vulnerability.
