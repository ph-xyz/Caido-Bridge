# Security policy

## Supported versions

| Version | Supported |
| --- | --- |
| 0.4.x | Yes |
| 0.3.1 and earlier | Upgrade recommended |

## Reporting a vulnerability

Do not open a public GitHub issue for a finding that contains an exploit,
credential, private target data, or another sensitive detail. Use
[GitHub private vulnerability reporting](https://github.com/ph-xyz/Caido-Bridge/security/advisories/new).
Repository maintainers must keep private vulnerability reporting enabled and
monitor incoming advisories. Include:

- affected version and platform;
- affected tool, script, or trust boundary;
- reproduction steps using non-sensitive test data;
- expected and observed behavior;
- security impact and any suggested mitigation;
- whether the finding has been disclosed elsewhere.

Please allow a reasonable period for acknowledgement, validation, and a fix
before public disclosure. Never send real Caido tokens, Runtime API keys,
tunnel IDs, DPAPI blobs, customer traffic, or target credentials in a report.

## Scope and responsibility

The maintainers can address vulnerabilities in CaidoBridge source, packaging,
and installation scripts. Availability, account policy, and vulnerabilities in
Caido, ChatGPT, the OpenAI Platform, or `tunnel-client` must be reported to the
respective vendor. Operators remain responsible for target authorization,
scope configuration, credential lifecycle, and reviewing Active Replay calls.

CaidoBridge is provided under the MIT License without warranty. It is an
independent project and is not officially affiliated with Caido or OpenAI.
