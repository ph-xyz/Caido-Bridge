# Migration from v0.3.1

CaidoBridge v0.4.0 is a public-product and installation release. The MCP core
and tool contracts remain those of PH Caido MCP v0.3.1.

## Renamed and preserved identifiers

| Purpose | v0.4.0 value |
| --- | --- |
| Executable | `CaidoBridge.exe` |
| Version output | `CaidoBridge v0.4.0` |
| State directory | `%LOCALAPPDATA%\PHCaidoMCP` |
| Tunnel profile | `ph-caido-mcp` |
| Scheduled task | `PH Caido MCP Tunnel` |
| Config schema | `schema_version: 1` |

The old executable is replaced by `CaidoBridge.exe`. The state directory,
profile, and task remain legacy compatibility contracts so the installer can
reuse v0.3.1 DPAPI credentials, tunnel association, Replay state, and
autostart without a destructive migration.

## Upgrade procedure

1. Keep the existing installation and ZIP backup.
2. Download and verify the v0.4.0 ZIP and official `tunnel-client` release.
3. Keep Caido open with the intended project selected.
4. Run the new `install.ps1`.
5. Run `doctor.ps1` and `status.ps1`.
6. In ChatGPT, refresh/reconnect the developer-mode app and verify the tool
   list. Active tools appear only if they were previously enabled.

The installer decrypts the existing DPAPI values only in the current-user
process, revalidates them, and writes the refreshed configuration with DPAPI.
It preserves `replay_enabled`; a missing legacy property defaults safely to
disabled. The tunnel profile is backed up before refresh.

## Rollback

If profile doctor, task registration, readiness, or final doctor fails, the
installer restores the old runtime, config, profile, and task automatically.
It reports failure and does not claim v0.4.0 success. A timestamped previous
runtime backup is retained after successful upgrade for operator recovery.

## Behavioral changes

- Public documentation and product output use CaidoBridge.
- A clean, guided installer and root-level operational wrappers are available.
- Updating the Caido token no longer risks resetting Active Replay state.
- Source now lives at repository root with module
  `github.com/ph-xyz/Caido-Bridge`.
- Active Replay, request mutation, evidence, and all core security contracts
  are otherwise unchanged.
