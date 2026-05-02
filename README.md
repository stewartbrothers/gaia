# gaia — Git AI Access

Token-trimmed, agent-friendly CLI and MCP server for Forgejo (and
eventually GitHub). Built so that LLM-driven agents can interact with
a forge over either shell tools or the Model Context Protocol without
burning tokens on the bloat that comes with raw REST responses.

`gaia` ships:

- **`gaia` CLI** — every read + write operation an AI agent or human
  typically needs against a forge, with output shaped for LLM
  consumption: JSON-by-default, `--fields` projection, paginated
  envelopes, and structured exit codes.
- **`gaia-mcp`** — a Model Context Protocol stdio server (HTTP/SSE in
  Phase 3) exposing every CLI operation as an MCP tool with the same
  envelope shape.
- A shared **`core/`** Go library with a `Provider` interface that
  backs both frontends. Forgejo first; GitHub provider in Phase 2.

## Quickstart

```bash
$ make build

$ ./bin/gaia auth forgejo https://git.example.com
Forgejo at https://git.example.com/api/v1
Visit ... to create a Personal Access Token.
Paste token: ****
✓ Authenticated as alice
✓ Saved to ~/.config/gaia/credentials.yaml (global)

$ ./bin/gaia whoami --format pretty
alice

$ ./bin/gaia --repo o/r --fields number,title,state issue list --state open
{ "schema_version": "1.0", "data": [...], "_truncated": false }

$ ./bin/gaia --repo o/r pr create --title "feat: ..." --head feature/x --base main
$ ./bin/gaia --repo o/r pr review 42 --state approve --body "ship it"
$ ./bin/gaia --repo o/r pr merge 42 --method squash
```

## Command surface (current)

```
gaia auth   forgejo | gh | status | logout
gaia issue  list | view | create | edit | close | reopen
            comment | comment-edit | comment-delete
gaia pr     list | view | diff | comments
            create | edit | close | reopen | comment-create
            merge | review | checkout
gaia label  list | create | edit | delete
gaia search <query>
gaia whoami | version
```

## Docs

- [`docs/auth.md`](docs/auth.md) — `gaia auth ...` flow
- [`docs/configuration.md`](docs/configuration.md) — config file layers
- [`docs/output-format.md`](docs/output-format.md) — envelope, `--fields`, pagination
- [`docs/exit-codes.md`](docs/exit-codes.md) — `0|2|3|4|5|6` matrix
- [`docs/mcp.md`](docs/mcp.md) — wiring `gaia-mcp` into MCP-aware clients
- [`docs/agent-guide.md`](docs/agent-guide.md) — dense pointers for AI agents
- [`docs/dogfood-comparison.md`](docs/dogfood-comparison.md) — empirical
  byte/token comparisons vs raw curl + tea

## Status

Phase 1 (Forgejo CRUD + CLI + MCP) is functionally complete. Phase 2
(GitHub provider) and Phase 3 (remote MCP transport) are next.

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
make build      # → bin/gaia, bin/gaia-mcp
make test       # full suite
make cover      # with per-function summary
make lint       # golangci-lint
```

## License

Apache-2.0. See [`LICENSE`](LICENSE).
