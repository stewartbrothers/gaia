# Changelog

All notable changes to gaia are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
with the standard 0.x carve-out: while on `0.x.y`, **breaking changes
to the public surface (CLI flags, MCP tool names, envelope shape, exit
codes) may land at minor bumps**. Once `1.0.0` ships, MAJOR bumps are
reserved for breaking changes only.

## [Unreleased]

Tracking issues for upcoming work:
- **Phase 4** (#4): SQLite cache (#42), indexed search (#43), webhook
  helpers (#85, #44), CI runs/logs (#45), NDJSON streaming (#46).
- **Distribution** (#5): GitHub mirror (#47), Homebrew tap (#49),
  upstream submission (#51).
- **Wider surface**: package registry support (#107), wiki support
  (#108).

## [0.1.0] — first developer-preview release

First tagged release. Phases 1–3 + initial Distribution work landed.
Pre-v1.0, expect minor-bump churn at the public surface.

### Core: providers + types

- Forgejo provider (#1 epic): full CRUD across issues, PRs, labels,
  releases, comments, search, reviews. CI summary reconciliation
  (`/commits/{sha}/status`). Trim-at-boundary so responses stay agent-
  shaped.
- GitHub provider (#2 epic): parity with Forgejo. CI summary uses
  `/commits/{sha}/check-runs` and rolls per-check status × conclusion
  into the same `types.CISummary` shape. ListIssues filters out PRs
  client-side. State reconciliation (`merged_at` → `state="merged"`)
  at the provider boundary. (#31–#37, #93)
- Trimmed `core/types`: deliberately omits URLs, avatar links, internal
  IDs, and other API bloat. Per-package coverage 83-90% across both
  providers post-#93.

### CLI surface (`gaia`)

- Identity: `version`, `whoami`, `auth forgejo|gh|status|logout`
- Issues: `list | view | create | edit | close | reopen | comment |
  comment-edit | comment-delete`
- PRs: `list | view | diff | comments | create | edit | close | reopen
  | comment-create | merge | review | checkout`
- Labels: `list | create | edit | delete`
- Releases: `list | view | create | edit | delete` (#84, PR #92)
- Search: `gaia search <query>` with cross-repo + per-repo modes,
  GitHub `is:issue`/`is:pr` qualifiers passed through verbatim.

### MCP server (`gaia-mcp`)

- **stdio transport** (Phase 1, #26 + #27): every CLI operation
  exposed as an MCP tool with the same envelope shape.
- **Streamable HTTP transport** (#39, PR #94): per the 2025-03-26 MCP
  spec. SIGTERM/SIGINT graceful shutdown. Configurable read-header /
  idle / shutdown timeouts.
- **Pass-through bearer auth** (#40 redesigned in #97): client sends
  forge PAT in `Authorization: Bearer`; gaia-mcp uses it verbatim
  upstream and stores nothing. Bind policy refuses non-loopback bind
  without `--allow-public-no-tls`. Audit log (token never logged).
- **/healthz** (liveness) + **/readyz** (forge ping with 5s deadline)
  on the same listener, no auth required (#41, PR #98).
- **MCP tools added since stdio**: `gaia_release_*` (PR #92).

### Distribution + deploy

- Multi-stage Dockerfile (~23 MB final, non-root uid 1000, fail-loud
  default CMD that refuses public bind without TLS-ack). HEALTHCHECK
  via busybox wget.
- `deploy/docker-compose.example.yml` + `deploy/nginx.conf` showing
  loopback-only and public+TLS patterns.
- `docs/deploy-mcp.md` with K8s manifest, rolling deploy story,
  log-aggregation guide, "what's NOT here" pointers.
- goreleaser multi-arch binaries (#48, PR #99): linux/darwin/windows
  × amd64/arm64. Forgejo Actions release workflow on tag push.
- `make release-snapshot` for local dry-runs.

### Project-local config

- `.gaia/config.yaml` (PR #104): non-secret defaults committable into
  the repo so `gaia issue list` / `gaia pr create` etc. work bare —
  no `--provider`, `--api-url`, or `--repo` on every call. Layered
  resolve: project > global > env > flags.
- `.gaia/credentials.yaml` (mode 0600, gitignored): per-project token
  override that shadows the global `~/.config/gaia/credentials.yaml`
  for one host inside one checkout.
- Structural gitignore protection for `.gaia/credentials*` (#105, PR
  #106). Doesn't depend on the runtime auth flow having been invoked.

### Auth + env vars

- Standard env-var fallback chains (#102, PR #103): forgejo honors
  `FORGEJO_TOKEN` then `GITEA_TOKEN` (the tea-CLI convention); github
  honors `GITHUB_TOKEN` then `GH_TOKEN`. Profile-pinned `token_env`
  still wins.
- `GITEA_TOKEN` adopted as the project's documented convention; the
  prior `GIT_FORGE_GITEA_TOKEN` is deprecated as a project-internal
  workaround that nothing else in the ecosystem recognizes.

### Tests

- ~76% project coverage at v0.1.0.
- Two-tier strategy for the GitHub provider (#38, PR #93): hand-rolled
  httptest tests + recorded api.github.com fixtures (8 captured
  responses from cli/cli, ~340 KB committed). Catches drift between
  hand-rolled JSON and what GitHub actually returns.

### Docs

- `docs/install.md` — pre-built binary / `go install` / source build
- `docs/auth.md` — credential layering, env-var fallbacks, project
  config, gitignore protection
- `docs/configuration.md` — config file shape, profile selection
- `docs/output-format.md` — envelope shape, `--fields` projection,
  pagination cursors
- `docs/exit-codes.md` — 0|1|2|3|4|5|6 matrix
- `docs/mcp.md` — MCP usage including HTTP transport guide
- `docs/deploy-mcp.md` — container deploy walkthrough
- `docs/agent-guide.md` — dense pointers for AI agents
- `docs/dogfood-comparison.md` — empirical byte/token comparisons vs
  raw curl + tea
- `docs/provider-parity.md` — method × forge × support-level matrix
- `docs/testing.md` — two-tier test strategy

### Known gaps (carry to 0.2.0+)

- Webhook config (#85, #44): only path that genuinely requires curl
  fallback. Filed as Phase 4.
- Per-tenant routing in HTTP transport: pass-through (PR #97) makes
  this moot; if a future deployment wants different forge identities
  per bearer token, that's a new feature.
- OS keychain backing for `credentials.yaml` (vs current 0600
  plaintext): `gh` does this, gaia doesn't yet.

[Unreleased]: https://github.com/stewartbrothers/gaia/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/stewartbrothers/gaia/releases/tag/v0.1.0
