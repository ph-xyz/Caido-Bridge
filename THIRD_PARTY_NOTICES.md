# Third-party notices

CaidoBridge includes or derives from third-party open-source software. Exact
upstream license texts for the reviewed versions are bundled in
[`THIRD_PARTY_LICENSES.txt`](THIRD_PARTY_LICENSES.txt) and are copied into
release archives.

## Source-derived work

Parts of the HTTP parsing, URL construction, MCP tool layout, scope matching,
and Caido SDK adaptation were derived from or informed by
[`c0tton-fluff/caido-mcp-server`](https://github.com/c0tton-fluff/caido-mcp-server)
at commit `7c41354efcab7baafebf3f9acc5e52977410dd05` (MIT).

## Go module inventory

This inventory matches the direct and indirect modules in `go.mod`. Versions
in `go.mod` and checksums in `go.sum` remain authoritative.

| Module | Relationship | Reviewed version | License |
| --- | --- | --- | --- |
| [`github.com/Khan/genqlient`](https://github.com/Khan/genqlient) | Direct | v0.8.1 | MIT |
| [`github.com/caido-community/sdk-go`](https://github.com/caido-community/sdk-go) | Direct | f03a805241ce | MIT |
| [`github.com/google/jsonschema-go`](https://github.com/google/jsonschema-go) | Direct | v0.4.3 | MIT |
| [`github.com/google/uuid`](https://github.com/google/uuid) | Direct | v1.6.0 | BSD-3-Clause |
| [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) | Direct | v1.7.0 | Apache-2.0 and MIT transition; documentation CC-BY-4.0 |
| [`github.com/segmentio/asm`](https://github.com/segmentio/asm) | Indirect | v1.1.3 | MIT |
| [`github.com/segmentio/encoding`](https://github.com/segmentio/encoding) | Indirect | v0.5.4 | MIT |
| [`github.com/vektah/gqlparser/v2`](https://github.com/vektah/gqlparser) | Indirect | v2.5.19 | MIT |
| [`github.com/yosida95/uritemplate/v3`](https://github.com/yosida95/uritemplate) | Indirect | v3.0.2 | BSD-3-Clause |
| [`golang.org/x/oauth2`](https://github.com/golang/oauth2) | Indirect | v0.35.0 | BSD-3-Clause |
| [`golang.org/x/sync`](https://github.com/golang/sync) | Indirect | v0.20.0 | BSD-3-Clause |
| [`golang.org/x/sys`](https://github.com/golang/sys) | Indirect | v0.41.0 | BSD-3-Clause |
| [`golang.org/x/time`](https://github.com/golang/time) | Indirect | v0.15.0 | BSD-3-Clause |

The separately downloaded OpenAI `tunnel-client` is not redistributed by
CaidoBridge. Its own license and release checksums must be reviewed from the
official upstream release.
