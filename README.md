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

## Install

```bash
# Download a tagged release for your platform (replace TAG + PLATFORM):
TAG=v0.1.0 PLATFORM=linux_x86_64
curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/${TAG}/gaia_${TAG}_${PLATFORM}.tar.gz"
tar -xzf "gaia_${TAG}_${PLATFORM}.tar.gz"
sudo install gaia gaia-mcp /usr/local/bin/

# Or via go install (Go 1.23+):
go install github.com/stewartbrothers/gaia/cmd/gaia@latest
go install github.com/stewartbrothers/gaia/cmd/gaia-mcp@latest

# Or build from source:
git clone https://github.com/stewartbrothers/gaia.git
cd gaia && make build
```

Full install guide including checksum verification:
[`docs/install.md`](docs/install.md). For the `gaia-mcp --http`
container deployment story: [`docs/deploy-mcp.md`](docs/deploy-mcp.md).

## Quickstart

```bash
$ gaia auth forgejo https://git.example.com
Forgejo at https://git.example.com/api/v1
Visit ... to create a Personal Access Token.
Paste token: ****
✓ Authenticated as alice
✓ Saved to ~/.config/gaia/credentials.yaml (global)

$ gaia whoami --format pretty
alice

$ gaia --repo o/r --fields number,title,state issue list --state open
{ "schema_version": "1.0", "data": [...], "_truncated": false }

$ gaia --repo o/r pr create --title "feat: ..." --head feature/x --base main
$ gaia --repo o/r pr review 42 --state approve --body "ship it"
$ gaia --repo o/r pr merge 42 --method squash
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

- [`docs/install.md`](docs/install.md) — pre-built binaries, `go install`, source build
- [`docs/auth.md`](docs/auth.md) — `gaia auth ...` flow
- [`docs/configuration.md`](docs/configuration.md) — config file layers
- [`docs/output-format.md`](docs/output-format.md) — envelope, `--fields`, pagination
- [`docs/exit-codes.md`](docs/exit-codes.md) — `0|2|3|4|5|6` matrix
- [`docs/mcp.md`](docs/mcp.md) — wiring `gaia-mcp` into MCP-aware clients
- [`docs/deploy-mcp.md`](docs/deploy-mcp.md) — `gaia-mcp --http` container deploy
- [`docs/chain.md`](docs/chain.md) — `gaia chain run` for multi-step workflows
- [`docs/agent-guide.md`](docs/agent-guide.md) — dense pointers for AI agents
- [`docs/dogfood-comparison.md`](docs/dogfood-comparison.md) — empirical
  byte/token comparisons vs raw curl + tea

## Status

**v0.1.0** — first developer-preview release. Phases 1–3 functionally
complete: Forgejo + GitHub providers, full CLI + MCP surface (stdio
and HTTP transports with pass-through bearer auth, healthz/readyz,
container deploy), goreleaser-driven multi-arch binaries.

### Versioning

[SemVer](https://semver.org). While on `0.x.y`, **breaking changes
to the public surface (CLI flag names, MCP tool names, envelope
shape, exit codes) may land at minor bumps**. See
[`RELEASING.md`](RELEASING.md) for the full convention and the
cut-a-release procedure; see [`CHANGELOG.md`](CHANGELOG.md) for
release notes.

### What's next

Phase 4 (cache #42, indexed search #43, webhook helpers #85/#44, CI
runs/logs #45, NDJSON streaming #46), wider Distribution (homebrew
#49, upstream submission #51), and broader surface (package
registry #107, wikis #108).

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
make build              # → bin/gaia, bin/gaia-mcp
make test               # full suite
make cover              # with per-function summary
make lint               # golangci-lint
make release-snapshot   # local goreleaser dry-run → dist/
```

## License

Apache-2.0. See [`LICENSE`](LICENSE).
