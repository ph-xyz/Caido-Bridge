# CaidoBridge v0.4.0 release-readiness report

Date: 2026-08-25  
Repository: `github.com/ph-xyz/Caido-Bridge`  
Status: audited source is public on protected `main`. No version tag or GitHub
Release has been published.

## Source and repository audit

- Repository hygiene scans found no committed credential, private key, tunnel
  ID, DPAPI store, log, dump, packaged binary, local configuration, or release
  archive.
- Reachable Git history uses GitHub's privacy-preserving noreply identity.
- CaidoBridge calls only the local Caido `/health` and `/graphql` endpoints.
  No Caido plugin is required.
- OpenAI `tunnel-client` remains an external dependency and is not
  redistributed.
- Go module dependencies are enumerated in `THIRD_PARTY_NOTICES.md`; exact
  upstream license texts are bundled in `THIRD_PARTY_LICENSES.txt` and copied
  into the release ZIP.
- GitHub Actions are pinned to commit SHAs and run with `contents: read`.
- Dependabot is configured for monthly Go module and GitHub Actions updates.

## Replay hardening in this candidate

- `scopeId` is required for scope checks, previews, and active Replay.
- Only the selected Caido scope preset is evaluated. Deny rules take precedence,
  an empty allowlist fails closed, and both `*` and `?` host wildcards are
  supported.
- Each preview creates a cryptographically random token that expires after two
  minutes and is consumed on its first active attempt.
- The token is bound to project ID, visible History request ID, scope ID, source
  fingerprint, and prepared request fingerprint. A changed mutation, different
  scope, expired token, or token reuse is blocked before target traffic.
- Output no longer claims that a session was preserved or that all secrets were
  redacted. It reports whether authentication material exists, whether sensitive
  header values were redacted, and that request-body redaction was not applied.

## Automated release gates

The CI workflow must pass all of the following on the final pull request head:

| Gate | Required result |
| --- | --- |
| `gofmt -l .` | No files |
| `go mod verify` | Pass |
| `go test ./...` | Pass |
| `go test -race ./...` | Pass |
| `go vet ./...` | Pass |
| PowerShell parser | Pass for every `.ps1` |
| Installer invariants | Pass |
| Repository hygiene scan | Pass |
| Windows amd64 build/version | Pass |
| Release ZIP build, manifest, and allowlist | Pass |

Any artifact hashes recorded before this hardening are intentionally considered
obsolete: source changes necessarily produce a different executable and ZIP.
Only hashes from the final tagged workflow may be published.

## Manual Windows acceptance checklist

Use a disposable Windows amd64 VM/user and non-production credentials.

- [ ] Verify the ZIP sidecar, extract it, and place a separately verified
      official `tunnel-client.exe` in `bin`.
- [ ] Confirm missing `tunnel-client` fails before state/profile/task changes.
- [ ] Install against a loopback Caido test project.
- [ ] Confirm protected configuration contains DPAPI ciphertext only and its ACL
      grants only the current user and Local System.
- [ ] Run doctor/status and confirm logs contain no token, key, or child output.
- [ ] Connect a test ChatGPT developer-mode app and verify eight tools.
- [ ] Enable Replay, refresh, verify ten tools, preview and execute only an
      authorized non-production request, then disable Replay.
- [ ] Confirm a changed mutation, reused token, expired token, wrong `scopeId`,
      and empty allowlist are blocked without target traffic.
- [ ] Reinstall v0.4.0 and upgrade a v0.3.1 fixture; confirm protected state is
      preserved.
- [ ] Force a late readiness failure and confirm runtime/config/profile/task
      rollback.
- [ ] Exercise all uninstall modes and confirm credentials are removed only by
      the explicit `All` choice.

## Source publication checklist

- [x] Confirm reachable commit metadata uses GitHub's privacy-preserving
      noreply identity.
- [x] Pass Linux Go checks and Windows build/installer checks on clean `main`.
- [x] Protect `main` with mandatory pull requests, both CI jobs, up-to-date
      branches, conversation resolution, and no bypass, force pushes, or
      deletions.
- [x] Configure repository description, focused topics, squash-only merging,
      and automatic deletion of merged head branches.
- [x] Enable the dependency graph, Dependabot alerts, security updates, and
      grouped security updates.
- [x] Change repository visibility to public.
- [x] Verify branch protection enforcement and enable private vulnerability
      reporting.

## Release checklist

- [ ] Complete the manual Windows acceptance checklist.
- [ ] Create annotated tag `v0.4.0` only from the reviewed `main` commit.
- [ ] Download the tag workflow artifact and independently verify its manifest,
      members, executable version, and sidecar hashes.
- [ ] Create the GitHub Release manually and attach the ZIP and checksum files.
- [ ] State that the executable is unsigned, `tunnel-client` is external,
      Active Replay is opt-in, and explicit authorization is required.

Publishing the audited source is separate from publishing a supported binary
release. The audited source is public. Do not create the version tag or GitHub
Release until the manual Windows checklist is complete.
