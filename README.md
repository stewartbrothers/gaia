# gaia — Git AI Access

Token-trimmed, agent-friendly CLI and MCP server for Forgejo (and eventually
GitHub). Built so that LLM-driven agents can interact with a forge over either
shell tools or the Model Context Protocol without burning tokens on the bloat
that comes with raw REST responses.

`gaia` ships:

- **`gaia` CLI** — every operation an AI agent or human typically needs against
  a forge, with output shaped for LLM consumption: JSON-by-default, `--fields`
  projection, paginated envelopes, and structured exit codes.
- **`gaia-mcp`** — an MCP server (stdio in Phase 1; HTTP/SSE in Phase 3)
  exposing the same operations as MCP tools.
- A shared **`core/`** Go library with a `Provider` interface that backs both
  frontends. Forgejo first; GitHub provider in Phase 2.

## Status

Pre-alpha. Roadmap and active work live in the repo's issue tracker — see the
five tracking epics below.

## Roadmap

| Phase | Tracker | Goal |
| ----- | ------- | ---- |
| 1     | [#1](../../issues/1) | Forgejo provider, full CLI surface, stdio MCP server |
| 2     | [#2](../../issues/2) | GitHub provider parity |
| 3     | [#3](../../issues/3) | Remote MCP transport (HTTP/SSE) |
| 4     | [#4](../../issues/4) | Cache, indexed search, webhook + CI helpers |
| —     | [#5](../../issues/5) | Distribution & upstreaming |

## Build

```bash
make build
```

Binaries land in `./bin/gaia` and `./bin/gaia-mcp`.

## License

Apache-2.0. See [`LICENSE`](LICENSE).
