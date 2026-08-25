# Security model

## Security objectives

CaidoBridge is designed to expose useful local Caido evidence while preventing
the MCP server from becoming a general-purpose network automation surface.
Observation is always available; target traffic requires a separate local
opt-in and per-call evidence and confirmation.

## Enforced controls

- `CAIDO_URL` accepts only absolute HTTP(S) loopback URLs without userinfo,
  path, query, or fragment.
- API redirects and origin changes are rejected. The Caido bearer token is
  added only to `POST /graphql` on the exact validated origin.
- Tokens are never accepted as command-line arguments, printed, logged, or
  returned by tools.
- Windows credentials use current-user DPAPI and an ACL limited to that user
  and Local System.
- Active Replay is disabled by default; active tools are registered only after
  protected local opt-in.
- Every project-scoped operation requires a canonical, existing, currently
  selected project ID and checks it before and after data access.
- History lookups use the visible decimal request ID and require an exact row.
- Raw request method, host, path and Host header must agree with Caido metadata.
- Active calls require the preview fingerprint, same origin and immutable Host.
- Each preview issues a cryptographically random, two-minute, one-use token
  bound to the exact project, History row, selected scope, source fingerprint,
  and prepared request fingerprint. Active execution consumes it before send.
- Every scope check and Replay names one exact Caido `scopeId`. Its denylist
  overrides its allowlist, and an empty allowlist fails closed.
- `Host`, `Content-Length`, and `Transfer-Encoding` cannot be mutated directly;
  body changes recalculate framing and reject chunked ambiguity.
- Structured mutations fail closed for absent, duplicated, or stale `from`
  values. Multiple mutations require a separate flag.
- Every send requires `confirmExecution=true`; potentially state-changing
  methods require an additional confirmation, and a live baseline for those
  methods requires another confirmation.
- Sensitive header values are preserved internally when necessary but redacted
  from previews and evidence when present. Bodies are size-bounded but are not
  redacted; outputs state both facts.
- No automatic fuzzing, brute force, crawling, batching, concurrency, hidden
  requests, or vulnerability/severity verdict exists.

## Threats mitigated

| Threat | Primary mitigation |
| --- | --- |
| Token exfiltration to a remote origin | Loopback and exact-origin transport guard |
| Redirect-based credential forwarding | Redirects disabled |
| Accidental Replay exposure | Tools absent until local opt-in |
| Stale or changed request replay | Identity checks plus one-use token bound to both fingerprints |
| Cross-project evidence | Canonical current-project guard before/after I/O |
| Out-of-scope request | Exact selected scope; deny precedence; empty allowlist fails closed |
| Host smuggling/change | Exactly one immutable Host header |
| Accidental state change | Explicit execution and state-changing confirmations |
| Secret disclosure in normal output | DPAPI, ACL, hidden input, header redaction |
| Unbounded offensive automation | One-request contracts and no loops/batch/concurrency |
| Supply/package contamination | Release manifest, exact ZIP allowlist, CI hygiene scan |

## Scope semantics

CaidoBridge evaluates exactly the scope preset selected by `scopeId`; it does
not union independent presets. Host globs support Caido's `*` and `?`
wildcards, deny rules take precedence, and an empty allowlist matches nothing.
It does not expand IP ranges or add URL-path semantics that Caido did not
provide. Scope is a technical guard, not proof of legal authorization.

## Residual risks

- Any Replay can affect the target. Confirmations cannot make a method safe or
  undo traffic.
- Caido project selection is global. A narrow race remains between the last
  check and a GraphQL mutation; post-send mismatch discards evidence but cannot
  retract the request.
- Request/response bodies may contain arbitrary application secrets that
  header-name redaction cannot identify reliably.
- DPAPI protects data at rest for the current user, not against malicious code
  already running as that user.
- `tunnel-client` and Caido are external trust dependencies with independent
  update and vulnerability lifecycles.
- The unsigned executable can trigger SmartScreen. SHA-256 detects byte changes
  but does not replace Authenticode publisher identity.
- GraphQL operations depend on Caido's current local API contract; future Caido
  releases can require compatibility updates.

## Operator responsibilities

Use only authorized targets, configure accurate Caido scopes, protect the
Windows account, verify release hashes and external binaries, keep Caido and
`tunnel-client` current, disable Active Replay after use, rotate expired or
exposed credentials, and review every state-changing confirmation.
