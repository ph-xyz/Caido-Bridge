# Contributing

CaidoBridge welcomes focused fixes, tests, and documentation improvements that
preserve its conservative security model.

## Before opening a pull request

1. Do not include captured customer traffic, tokens, tunnel IDs, profiles,
   logs, executables, ZIPs, or DPAPI files.
2. Keep Active Replay opt-in and preserve project, request identity, scope,
   Host, fingerprint, confirmation, and redaction guards.
3. Do not add automated crawling, fuzzing, brute force, batching, concurrency,
   or vulnerability verdicts.
4. Add tests for every behavior change.

Run:

```powershell
gofmt -w .
go mod verify
go test ./...
go vet ./...
.\scripts\installer-tests.ps1
.\scripts\verify-repository.ps1
```

On a supported CGO host, also run `go test -race ./...`. Release changes must
pass `.\scripts\build-release.ps1` and inspection of the resulting archive.

Use small commits and explain security-boundary changes explicitly. Report
sensitive findings through the private process in [SECURITY.md](SECURITY.md),
not a public issue.
