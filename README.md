# gaia — Git AI Access

> **Latest: v0.2.8** — released 2026-05-07. See [`CHANGELOG.md`](CHANGELOG.md) for release notes.

Token-trimmed, agent-friendly CLI and MCP server for **Forgejo,
Gitea, and GitHub**. Built so that LLM-driven agents can interact with
a forge over either shell tools or the Model Context Protocol without
burning tokens on the bloat that comes with raw REST responses.

> Forgejo is a hard fork of Gitea with a compatible REST API. gaia
> ships a single `forgejo` provider that targets either —
> `gaia auth forgejo https://your-gitea.example.com/api/v1` works the
> same as it does against a Forgejo instance.

`gaia` ships:

- **`gaia` CLI** — every read + write operation an AI agent or human
  typically needs against a forge, with output shaped for LLM
  consumption: JSON-by-default, `--fields` projection, paginated
  envelopes, structured exit codes, NDJSON streaming, and
  multi-step workflow chains.
- **`gaia-mcp`** — a Model Context Protocol server with both stdio
  and streamable HTTP transports, pass-through bearer auth (the
  client's forge PAT travels untouched; gaia-mcp stores nothing).
- A shared **`core/`** Go library with a `Provider` interface that
  backs both frontends. The same surface — issues, PRs, comments,
  labels, releases, search, packages, wikis, webhooks — works
  against Forgejo, Gitea, and GitHub.

## Install

**One-line installer** (macOS + Linux, x86_64 + arm64):

```bash
curl -fsSL https://raw.githubusercontent.com/stewartbrothers/gaia/main/scripts/install.sh \
  | bash
```

**Homebrew** (macOS + Linux):

```bash
brew tap stewartbrothers/gaia https://github.com/stewartbrothers/gaia
brew install gaia
```

**Container** (for `gaia-mcp --http` server deployments):

```bash
docker pull ghcr.io/stewartbrothers/gaia-mcp:latest
```

**Build from source**:

```bash
git clone https://github.com/stewartbrothers/gaia.git
cd gaia && make build
```

Full install guide including checksum verification:
[`docs/install.md`](docs/install.md). For `gaia-mcp --http`
container deployments: [`docs/deploy-mcp.md`](docs/deploy-mcp.md).

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
gaia auth     forgejo | gh | status | logout
gaia issue    list | view | create | edit | close | reopen
              comment | comment-edit | comment-delete
gaia pr       list | view | diff | comments | ci-wait
              create | edit | close | reopen | comment-create
              merge | review | checkout
gaia label    list | create | edit | delete
gaia release  list | view | create | edit | delete | publish
gaia packages list | view | delete | upload
gaia wiki     list | view | search | edit | delete
gaia webhook  list | view | create | edit | delete
              deliveries | redeliver | test
gaia chain    run | resume | list | abort
gaia cache    nuke
gaia search   <query>
gaia whoami | version
```

Every command supports `--format json|pretty|ndjson` and `--fields`
projection; mutating commands honour `--dry-run` where it makes
sense. See [`docs/output-format.md`](docs/output-format.md) for the
envelope shape and [`docs/exit-codes.md`](docs/exit-codes.md) for
the structured exit-code matrix (0 success, 2 usage, 3 not-found,
4 auth, 5 rate-limit, 6 network, 7-11 CI/merge/policy outcomes).

## Docs

- [`docs/install.md`](docs/install.md) — pre-built binaries, Homebrew, source build
- [`docs/auth.md`](docs/auth.md) — `gaia auth ...` flow
- [`docs/configuration.md`](docs/configuration.md) — config file layers
- [`docs/output-format.md`](docs/output-format.md) — envelope, `--fields`, pagination
- [`docs/exit-codes.md`](docs/exit-codes.md) — `0|2|3|4|5|6` matrix
- [`docs/mcp.md`](docs/mcp.md) — wiring `gaia-mcp` into MCP-aware clients
- [`docs/deploy-mcp.md`](docs/deploy-mcp.md) — `gaia-mcp --http` container deploy
- [`docs/chain.md`](docs/chain.md) — `gaia chain run` for multi-step workflows
- [`docs/agent-guide.md`](docs/agent-guide.md) — dense pointers for AI agents
- [`docs/dogfood-comparison.md`](docs/dogfood-comparison.md) — headline
  byte/token wins vs raw curl + tea; per-resource measurements live
  under [`bench/`](bench/README.md)

## What gaia does

- **Forgejo, Gitea, and GitHub** — the same CLI and MCP tool surface
  works across all three. Issues, pull requests, comments, labels,
  releases, search, packages, wikis, webhooks. (GitHub wikis use a
  clone-cache fallback since they aren't REST-served upstream.)
- **Token-cheap output** — JSON-by-default with `--fields` projection,
  structured exit codes, paginated envelopes. Built so an LLM agent's
  context window doesn't burn on REST bloat.
- **MCP server** — `gaia-mcp` over stdio or streamable HTTP, with
  pass-through bearer auth (the client's PAT travels untouched).
- **Local cache** — read responses are cached with TTL + ETag
  conditional GET. Repeat reads of the same resource are roughly an
  order of magnitude faster.
- **NDJSON streaming** for list commands — output starts flowing on
  the first record instead of buffering until the last page.
- **Multi-step chains** — `gaia chain run` orchestrates linear,
  parallel, and conditional workflows so an agent isn't burning
  tool turns on sequencing.
- **Security-aware envelope** — forge-supplied content (issue/PR
  bodies, comments, wikis) is tagged `_trust: "external"` and
  wrapped in delimiters in pretty output, so agents can be told to
  treat it as data, not instructions.

### Versioning

[SemVer](https://semver.org). While on `0.x.y`, **breaking changes
to the public surface (CLI flag names, MCP tool names, envelope
shape, exit codes) may land at minor bumps**. See
[`RELEASING.md`](RELEASING.md) for the cut-a-release procedure and
[`CHANGELOG.md`](CHANGELOG.md) for release-by-release notes.

### Coming next

- Cross-resource indexed search across issues + PRs (today's
  `gaia search` hits the live forge each time).
- `gaia ci runs` / `gaia ci logs` helpers for richer CI inspection
  beyond `gaia pr ci-wait`.

The full backlog lives in the [issue tracker](https://github.com/stewartbrothers/gaia/issues).

## Build

```bash
make build              # → bin/gaia, bin/gaia-mcp
make test               # full suite
make cover              # with per-function summary
make lint               # golangci-lint
make release-snapshot   # local goreleaser dry-run → dist/
```

## Source

The project is hosted on GitHub at
[`github.com/stewartbrothers/gaia`](https://github.com/stewartbrothers/gaia).

- **Issues, PRs, releases** — [github.com/stewartbrothers/gaia](https://github.com/stewartbrothers/gaia)
- **Homebrew tap** — `brew tap stewartbrothers/gaia https://github.com/stewartbrothers/gaia`

## License

Apache-2.0. See [`LICENSE`](LICENSE).
