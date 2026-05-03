# Changelog

All notable changes to gaia are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
with the standard 0.x carve-out: while on `0.x.y`, **breaking changes
to the public surface (CLI flags, MCP tool names, envelope shape, exit
codes) may land at minor bumps**. Once `1.0.0` ships, MAJOR bumps are
reserved for breaking changes only.

## [Unreleased]

### Fixed

- Trust-tagged JSON output now preserves struct-declaration field order
  (was alphabetical since #146). Closes #148; restores the historical
  wire-shape ordering that pre-#146 callers relied on for canonical
  serialisation / hash-keyed caching.

### Security

- Chain runner: substituted variable values are now shell-quoted
  before insertion into the run command (#135). Hostile vars /
  captures can no longer inject shell metacharacters. Existing
  chains that wrapped `${var}` references in their own `"..."` or
  used `${var}` as multi-token shell input need to drop the manual
  quoting (the shell-quoting is now automatic) or wrap with
  `sh -c "${var}"` in their own step. The bundled chain scenarios
  and `docs/chain.md` example are updated.
- GitHub wiki cache: validates owner/repo/slug against an allowlist
  (`[A-Za-z0-9_.-]+` minus `.`, `..`, hidden-name prefixes,
  separators, null bytes) before joining into filesystem paths
  (#136); path-traversal segments are rejected with a clear error.
- GitHub wiki cache: git auth header now travels via `GIT_CONFIG_*`
  env vars instead of in the URL passed to argv (#137); tokens are
  no longer visible in `ps`, `/proc/<pid>/cmdline`, or process
  accounting. Requires git 2.31+ for `GIT_CONFIG_COUNT` support.
  `runGit` error formatting also scrubs both the joined argv and
  combined output through `scrubToken` as defence in depth.
- MCP tool envelopes / CLI output: fields containing user-provided
  forge content (issue bodies, PR bodies, comments, wiki content,
  release bodies, titles, search snippets, etc.) now carry
  `_trust: "external"` markers in JSON and `<<<EXTERNAL` /
  `EXTERNAL>>>` delimiters in pretty output. `--no-external-markers`
  opts out of the pretty wrapping for tooling that processes raw
  output; JSON output is always tagged. Mitigates indirect prompt
  injection (#146). See `docs/agent-guide.md` "Threat model: tool
  results carry untrusted content" for the recommended
  system-prompt snippet.
- gaia-mcp `/readyz` no longer drains the host's forge rate limit
  on unauthenticated requests (#139). The endpoint is now
  liveness-equivalent (200 "ready" while the listener is bound,
  no upstream call). Operators who need to monitor forge
  reachability should run a CLI probe (`gaia whoami`) from their
  monitoring host — real MCP callers already see upstream errors
  on their own `/mcp` requests.

### Wire-shape change (consumers must update)

- Trust-tagged fields (above) now serialise as
  `{"_trust":"external","_value":"<text>"}` rather than as a bare
  string. Consumers decoding gaia JSON into typed structs need to
  update their decode shapes — see the in-tree test pattern in
  `internal/cli/issue_test.go` (`trustExternal` helper).
- The marshal walker that applies the trust tag emits objects via a
  reflection pass. Initially this changed `data.*` field order from
  struct-declaration order to alphabetical (object keys in JSON are
  unordered per RFC 8259, so consumers that key by name still work);
  consumers that rely on byte-identical serialisation needed to
  re-baseline. **Resolved in #148** — the walker now preserves
  struct-declaration order via an ordered-object marshaler, so the
  historical wire ordering is back. This second drift is the final
  one; consumers that re-baselined for the alphabetical interim
  should re-baseline once more against declaration order.

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
