# gaia — Git AI Access

> **Latest: v0.2.0** — released 2026-05-03. Phase 4 features (cache,
> NDJSON streaming, webhooks, packages, wikis), chain primitives
> (parallel + composition), substantial security hardening pass.
> See [`CHANGELOG.md`](CHANGELOG.md#020--2026-05-03) for the full
> notes and [`bench/`](bench/README.md) for measured byte/token wins.

Token-trimmed, agent-friendly CLI and MCP server for **Forgejo and
GitHub**. Built so that LLM-driven agents can interact with a forge
over either shell tools or the Model Context Protocol without burning
tokens on the bloat that comes with raw REST responses.

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
  backs both frontends. Forgejo and GitHub providers at parity for
  issues, PRs, comments, labels, releases, search, packages, wikis,
  webhooks.

## Install

```bash
# Homebrew (macOS + Linux):
brew tap Gerwood/gaia https://github.com/stewartbrothers/gaia
brew install gaia

# Or download a tagged release for your platform (replace TAG + PLATFORM):
TAG=v0.2.0 PLATFORM=linux_x86_64
curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/${TAG}/gaia_${TAG}_${PLATFORM}.tar.gz"
tar -xzf "gaia_${TAG}_${PLATFORM}.tar.gz"
sudo install gaia gaia-mcp /usr/local/bin/

# Or via go install (Go 1.25+):
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

- [`docs/install.md`](docs/install.md) — pre-built binaries, `go install`, source build
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

## Status

**v0.2.0** — second developer-preview release. Phases 1-3 complete +
most of Phase 4 shipped:

- **Forgejo + GitHub providers at parity** for issues, PRs, comments,
  labels, releases, search, packages, wikis, webhooks (clone-cache
  fallback for GitHub wikis since they aren't REST-served).
- **CLI + MCP** with stdio + streamable HTTP transports, pass-through
  bearer auth, healthz/readyz, container deploy.
- **Local SQLite cache** with TTL + ETag/If-Modified-Since
  conditional GET (~820× speedup on cache-bench).
- **NDJSON streaming output** for list commands (~150× faster
  time-to-first-byte).
- **Chain primitives** complete (linear → yield/resume →
  parallel/for_each/composition); `pr-create-and-land` canned
  chain measures **81% byte / 86% tool-turn reduction** vs the
  multi-call agent flow.
- **Security hardening pass** — chain shell-injection closed,
  prompt-injection markers, env scrub, govulncheck gate, SHA-pinned
  CI actions, `SECURITY.md`.
- **Distribution infrastructure** — Homebrew tap, GitHub mirror,
  release workflow polish.
- **Goreleaser-driven multi-arch binaries** + per-resource measured
  byte/token baselines under [`bench/`](bench/README.md).

### Versioning

[SemVer](https://semver.org). While on `0.x.y`, **breaking changes
to the public surface (CLI flag names, MCP tool names, envelope
shape, exit codes) may land at minor bumps**. See
[`RELEASING.md`](RELEASING.md) for the full convention and the
cut-a-release procedure; see [`CHANGELOG.md`](CHANGELOG.md) for
release notes.

### What's next (v0.3.0 milestone)

Remaining Phase 4 work plus follow-ups from this cycle:

- **#43** Cross-resource indexed search (depends on the cache
  layer that just landed).
- **#45** `gaia ci runs` / `gaia ci logs` helpers.
- **#51** Forgejo upstream submission (process work).
- **#153** Wire remaining `Get<Resource>` methods through
  `GetCached` (today only `GetIssue` and `GetPullRequest` are
  cached).

## Roadmap

| Phase | Tracker | Goal | Status |
| ----- | ------- | ---- | ------ |
| 1     | [#1](../../issues/1) | Forgejo provider, full CLI surface, stdio MCP server | ✓ shipped (v0.1.0) |
| 2     | [#2](../../issues/2) | GitHub provider parity | ✓ shipped (v0.1.0) |
| 3     | [#3](../../issues/3) | Remote MCP transport (HTTP/SSE) | ✓ shipped (v0.1.0) |
| 4     | [#4](../../issues/4) | Cache, indexed search, webhook + CI helpers | mostly shipped (v0.2.0); #43 + #45 remain |
| —     | [#5](../../issues/5) | Distribution & upstreaming | mostly shipped (v0.2.0); #51 remains |

## Build

```bash
make build              # → bin/gaia, bin/gaia-mcp
make test               # full suite
make cover              # with per-function summary
make lint               # golangci-lint
make release-snapshot   # local goreleaser dry-run → dist/
```

## Mirror

The canonical repo lives on a self-hosted Forgejo instance at
[`github.com/stewartbrothers/gaia`](https://github.com/stewartbrothers/gaia).
A public, read-only mirror is maintained at
[`github.com/stewartbrothers/gaia`](https://github.com/stewartbrothers/gaia) for
discoverability.

- **Issues, PRs, releases** — open on the Forgejo instance. The
  GitHub mirror does not accept patches.
- **Code browsing, `go install`, drive-by reading** — either side
  works.
- **Tags + main** mirror across to GitHub automatically (see
  [`docs/mirroring.md`](docs/mirroring.md) for the operator runbook).
  Release artifacts are attached to the Forgejo release; the Homebrew
  tap (#49) consumes them directly.

## License

Apache-2.0. See [`LICENSE`](LICENSE).
