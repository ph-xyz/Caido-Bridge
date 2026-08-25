# Installation and lifecycle details

The main [README](../README.md#clean-installation) contains the complete user
walkthrough. This document records the installer's transaction and recovery
behavior for operators and maintainers.

## Preflight boundary

Before writing `%LOCALAPPDATA%`, a profile, or a scheduled task, `install.ps1`:

1. requires Windows amd64;
2. verifies every CaidoBridge release file against `SHA256SUMS.txt` and rejects
   unexpected files, except a user-supplied `bin\tunnel-client.exe`;
3. checks `CaidoBridge.exe version` for exactly `CaidoBridge v0.4.0`;
4. resolves `tunnel-client` in explicit path, release `bin`, release root,
   `PATH`, then the existing installed runtime;
5. validates the external binary can report a version;
6. rejects non-loopback Caido URLs and requires Caido `/health` readiness;
7. obtains a valid tunnel ID and hidden credentials;
8. runs the source binary doctor, including authenticated project and
   read-only HTTP History checks.

Missing `tunnel-client` fails before secret prompts and prints its official
latest-release URL, expected filename, destination, and rerun command.

## Transaction

After preflight, the installer:

1. stages the runtime under the legacy state directory;
2. stops only the matching scheduled task, tunnel profile, and exact legacy
   server path;
3. swaps the previous runtime to a timestamped backup;
4. refreshes and validates the named stdio tunnel profile;
5. writes both secrets with current-user DPAPI and restrictive ACLs;
6. registers a limited, hidden logon task and starts it;
7. waits for `/readyz` and runs final CaidoBridge and tunnel doctors.

No token or key is placed in a process argument. The scheduled supervisor
loads them into the child process environment only.

## Rollback and idempotency

On failure after the transaction starts, the installer stops the new task,
removes only the validated new runtime, restores the previous runtime, profile,
configuration and task, and restarts the previous installation when it
existed. A clean installation failure removes newly created state when empty.

Rerunning the same release refreshes validated files without changing the
profile name, scheduled task name, state path, or protected Active Replay flag.
Running a newer release uses the same flow as an upgrade.

## External tunnel-client

CaidoBridge copies the user-provided `tunnel-client.exe` into the user's local
runtime for stable autostart. That is a local installation operation, not
redistribution: the executable is never committed, placed in the CaidoBridge
release ZIP, or covered by CaidoBridge's manifest.

Obtain it only from [OpenAI's tunnel-client releases](https://github.com/openai/tunnel-client/releases/latest)
or the Platform Tunnels download, and validate the official checksum.

## Uninstall levels

- **Autostart:** removes only the Windows task. Runtime and protected
  credentials remain for manual use or repair.
- **Runtime:** additionally removes installed binaries/scripts. The DPAPI
  config remains so reinstall can preserve settings.
- **All:** additionally removes logs, tunnel profile, state directory, and
  DPAPI ciphertext. Every destructive scope uses PowerShell confirmation.
