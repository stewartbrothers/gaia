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

- **CI: golden scenarios stop opening sqlite cache per-run.** The
  `clearGaiaEnv` fix from #303 set `GAIA_CACHE_ENABLED=false` in
  the shared CLI test helper, but golden scenarios (in
  `cmd/gaia/main_test.go` and `internal/cli/golden_scenarios_test.go`)
  don't go through that helper — each scenario invokes the gaia
  CLI in-process and picked up the default cache-enabled config.
  Result: 30+ sqlite opens per package on Linux CI, each paying
  the modernc-pure-Go fsync tax that pushed `internal/cli` past
  the per-package 10-min `go test` timeout. Fix: harness-level
  `t.Setenv("GAIA_CACHE_ENABLED", "false")` in both scenario
  harnesses, mirroring the rationale of #303. Scenarios that
  explicitly test cache behaviour can re-enable it via
  `stage.env`. Closes #319.

## [0.5.0] — 2026-05-21

Feature release. Three new top-level commands (`milestone`,
`config doctor`, `@me` sentinel on `issue list`), one new type
field (`HTMLURL` on Issue + PullRequest), one CI hardening
(GAIA_CACHE_ENABLED env), and one workflow-process tightening
(rule 1 in CLAUDE.md mandates fresh-main before branching). No
breaking changes to the public surface.

### Added

- **`gaia milestone list | view | create | edit | delete | issues`**
  — new top-level command for managing repo milestones. Available
  on both Forgejo and GitHub providers, with parallel MCP tool
  exposure (`milestone_list`, `milestone_view`, etc.). Includes a
  `--state` filter on `list` and an `issues` subcommand that
  returns the issues attached to a given milestone ID. Useful for
  sprint planning, release roll-ups, and any workflow where
  multiple issues need to be grouped against a deadline. Closes
  #258.

- **`gaia config doctor`** — new diagnostic that lints the resolved
  config + credentials for setup smells before they bite. Flags
  multi-project safety issues (global `default_profile` /
  `default_repo` overrides), credential file hygiene
  (`credentials.yaml` mode > 0600, missing `.gitignore` for
  project-scoped credentials), profile coherence (missing
  `provider` or `api_url`, dangling `default_profile` references),
  and env-vs-stored-credential overlap. Each finding carries a
  stable `code` for greppable CI gating, a level (`ERR` / `WARN` /
  `INFO`), and a one-line remediation. `--strict` promotes WARN to
  ERR; `--quiet` exits non-zero on ERR only; `--format json`
  returns the standard envelope. Closes #277.

- **`gaia issue list --assignee @me` and `--author @me`** — sentinel
  value that resolves to the configured user's login via one extra
  `Whoami` call. Matches the gh / tea convention. Both flags
  being `@me` still costs one lookup. Literal logins skip the
  resolver entirely. Closes #299.

- **`HTMLURL` field on `types.Issue` and `types.PullRequest`** —
  mirrors `WorkflowRun.HTMLURL`. Threaded through both Forgejo and
  GitHub providers on list + view paths. Eliminates the
  brittle reconstruct-from-API-base workaround for the common
  "give me the URL to share with a human" use case. Default-on;
  `--fields` projection lets callers who don't need it drop it.
  Adds ~72 bytes per record. Closes #305.

### Fixed

- **`gaia chain run` now inherits the caller's tool-environment** —
  chain `run:` steps used to be invoked under a scrubbed env that
  was deliberately narrow (only `PATH`, `HOME`, `USER`, `LOGNAME`,
  `LANG`, `LC_ALL`, `TERM`). The scrub correctly prevented forge
  tokens / cloud creds from leaking, but it also stripped the
  well-known *non-secret* activation vars set by `venv`, `nvm`,
  `pyenv`, `asdf`, and the Go toolchain. Net effect:
  `make ci-parity` (and any other chain step that wraps
  language-specific tooling) silently picked up the wrong
  interpreter or failed outright, even though the same command
  worked in the operator's terminal. The allowlist now passes
  through `VIRTUAL_ENV`, `NVM_*`, `PYENV_*`, `ASDF_*`, `GO*`,
  `JAVA_HOME`, `RUSTUP_HOME`, `CARGO_HOME`, plus prefix-matched
  `LC_*` / `XDG_*` / `CONDA_*` families. Forge tokens
  (`GITEA_TOKEN`, `FORGEJO_TOKEN`, `GH_TOKEN`, `GITHUB_TOKEN`),
  cloud creds (`AWS_*`, `GCP_*`, `AZURE_*`), and arbitrary
  operator-scope vars are still stripped — the security
  contract from #140 is unchanged. Closes #247.

- **CI: `internal/cli` no longer times out on the per-package
  10-min `go test` limit.** Every CLI test that called
  `cli.NewRootCmd() + Execute()` was opening a real on-disk
  sqlite cache DB via `forgebuilder.Build → sqlite.Open` and
  never closing it. modernc/pure-Go sqlite + Linux fsync was slow
  enough that the cumulative cost pushed the suite over the
  timeout. Fix: honor `GAIA_CACHE_ENABLED=false` as a third path
  to disable the cache (alongside `--no-cache` flag and
  `cache.enabled` YAML key), and set it in the shared
  `clearGaiaEnv` test helper so CLI tests that don't exercise the
  cache stop paying the open cost. Cache-specific tests
  (`internal/cli/cache_test.go`, `core/forgejo/cache_test.go`)
  don't use the helper and continue to validate the caching
  layer. Closes #303.

### Documentation

- **CLAUDE.md rule 1 now mandates `git fetch origin && git switch
  main && git pull --ff-only` before creating a feature branch**,
  with an explicit `--ff-only` to surface a diverged local main
  as a real bug instead of papering over it with a merge commit.
  Includes a "why this rule exists" footnote pointing at the
  cost case (PR #300 needed a rebase because the branch was cut
  from a stale local main while PR #297 had landed earlier the
  same day). Closes #301.

- **`docs/agent-guide.md` expanded** to cover the eight top-level
  commands that had landed without entries in the agent-facing
  quick-start (closes #271), plus rows for the new `milestone`,
  `config doctor`, `@me` sentinel, and HTMLURL projection use
  cases that landed this cycle.

- **Provider contract + first ADR** — `docs/provider-contract.md`
  pins the cross-cutting contract every `core.Provider`
  implementation must satisfy (response shape, error mapping,
  pagination, cache semantics, trust tagging) so future provider
  additions have a single source of truth instead of cargo-culting
  from `core/forgejo/`. `docs/adr/0001-internal-interfaces.md`
  opens the ADR series with the project's rule of thumb for when
  to introduce an internal Go interface vs. depending on a
  concrete type (PR #308).

### Tests

- **`config-doctor` golden scenarios for the two finding codes
  missing from the initial PR** (`profile-no-provider`,
  `default-profile-missing`). Brings golden-test coverage of
  doctor findings to 100%. Closes the follow-up issue
  identified during #277's review.

## [0.4.1] — 2026-05-12

Maintenance release. One user-facing fix (`brew install gaia` was
broken on v0.4.0 due to a Formula/gaia.rb checksum mismatch); the rest
of the work hardens the release pipeline so the v0.4.0-class breakage
can't repeat.

### Fixed

- `Formula/gaia.rb` sha256 checksums for v0.4.0 now match the
  archives published to the GitHub release. The v0.4.0 cut hit a
  release-workflow failure that necessitated a manual artifact
  re-upload from a locally-built `dist/`, producing binaries with
  different checksums than the original goreleaser-CI build. Brew
  users hit checksum mismatches on every install until this synced
  the formula to the actually-published bytes. Closes the active
  breakage; the cluster of pipeline issues that produced this trap
  is documented in #284. (PR #283)

### Internal — release pipeline hardening

The bulk of this release; no user-visible behaviour changes.

- **Pre-flight deploy-key write probe** — `release.yml` now SSH-probes
  the `GORELEASER_TAP_DEPLOY_KEY` against Forgejo before running
  goreleaser. Read-only keys fail in < 5 s with an actionable
  "fix in Forgejo UI" message; previously the failure surfaced 2+
  minutes into the run after the brew tap push was attempted.
  Closes #284 (failure A). (PR #286)
- **README badge bump race fix** — the README-bump step now refreshes
  `origin/main` before pushing, so an earlier in-workflow push (brew
  tap formula bump) doesn't make the README push non-fast-forward.
  Closes #284 (failure B). (PR #286)
- **Cosmetic-step resilience** — `Move latest tag` and `Bump README
  latest-version badge` are now `continue-on-error: true`. A
  cosmetic failure surfaces as a `::warning::` instead of tanking
  the downstream GHCR / mirror / GitHub publish steps. (PR #286)
- **`RELEASING.md` recovery procedure** — new "If the workflow fails
  partway through" section codifying: re-run the workflow first;
  never manually rebuild artifacts locally (that's the trap PR #283
  fixed); never reach for `gh release` as a workaround. Closes #285.
  (PR #286)
- **`scripts/cut-release.sh` Forgejo URL fix** — the post-tag-push
  message previously printed `https://github.com/.../actions` as the
  workflow URL. Now derives the Forgejo Actions URL from
  `.gaia/config.yaml`'s `api_url` and `default_repo`. (PR #286)
- **PR-time release smoke test** — new
  `.forgejo/workflows/release-smoke.yml` runs the build half of the
  release pipeline (`goreleaser --snapshot`, Dockerfile build, version
  injection) on every PR that touches release-infrastructure files.
  Catches goreleaser config drift, Dockerfile breakage, shell-script
  parse errors, and platform-target dropping out of the artifact
  matrix before they reach a real tag. Closes #288. (PR #290)
- **`brew_gate` + deploy-key probe consolidation** — merged the two
  adjacent steps into one `brew_gate` step that handles both
  PRESENCE (skip if secret unset) and WRITE PROBE (verify the
  registered key has write access). Same behaviour, simpler shape,
  no drift risk between the two checks. Closes #289. (PR #291)
- **`release.yml` split into 4 parallel jobs** — `verify` →
  `build-and-publish` plus parallel `ghcr` and `mirror`. A failure
  in one publish surface no longer tanks the others; each downstream
  job is independently re-runnable from the Forgejo Actions UI.
  Closes #287. (PR #292)

## [0.4.0] — 2026-05-11

New top-level `gaia gitignore` command, two MCP resources (`gaia://learn`
and `gaia://gitignore`), and a substantial Forgejo Actions provider fix
that aligns IDs with what the UI shows.

### Breaking

- **`gaia actions list|view`**: the `id` field is now the **user-facing
  run number** (matching the UI URL — e.g. `/actions/runs/362`) rather
  than the internal database task ID. The internal ID is exposed as
  `run_id` for callers that need it for follow-up API calls. Agents
  that hard-coded the previous `id` values must re-read after upgrading.
  Closes #261 / #263, in #269.

### Added

- **`gaia gitignore`** — new top-level command. `gaia gitignore` prints
  the recommended `.gitignore` block for any project that uses gaia
  (covers `.gaia/credentials*` plus the Phase 9 insights DB siblings).
  `gaia gitignore --check` audits an existing `.gitignore` and exits
  non-zero with the list of missing entries — pair with `--quiet` for
  CI gating. The same content is exposed as the MCP resource
  `gaia://gitignore` (MIME `text/plain`). Closes #249, in #273.
- **`gaia://learn`** MCP resource — exposes the agent guide
  (`docs/agent-guide.md`) as `text/markdown` at `gaia://learn`. Agents
  driving `gaia-mcp` (stdio or streamable HTTP) can `resources/read`
  the agent-onboarding briefing without shelling out to the CLI.
  Closes #245, in #274.

### Changed

- **`gaia actions logs|rerun`**: rather than calling endpoints that
  don't exist on Forgejo v15 (and 404'ing confusingly), these now
  return a clear "endpoint not supported on this Forgejo version"
  error that includes the run's HTML URL so callers have an
  actionable next step. Sibling issues #266 (logs) and #267 (rerun)
  track upstream Forgejo for when the API exposes these. In #269.

### Fixed

- **`gaia actions view`**: most fields (`workflow_name`, `branch`,
  `head_sha`, `actor`, timestamps) were returning zero-valued because
  the struct tags didn't match Forgejo's wire shape. Re-grounded the
  whole `core/forgejo/actions.go` mapping against the Forgejo v15.0.1
  Swagger + source + live curl. Closes #263, in #269.
- **Release workflow**: `Dockerfile` `GO_VERSION` default now threads
  from the `setup-go` input rather than being hardcoded, preventing
  Go-version skew when the workflow updates Go but the Dockerfile
  didn't get bumped in lockstep. Closes #259, in #264.
- **Release workflow**: dropped `--skip=publish` from
  `goreleaser release` so the Homebrew tap auto-bumps. The Forgejo
  release record is still opted out via `release: disable: true` in
  `.goreleaser.yml` — `gaia release publish` creates the release
  record after artifacts are built. Closes #260, in #265.

### Tests

- Anti-rot test for `docs/agent-guide.md` command coverage —
  `TestAgentGuideMentionsEveryTopLevelCommand` fails if a new
  top-level `gaia` command merges without at least a substring
  mention in the agent guide. Tolerates the documented baseline
  (tracked by #271) until each remaining command is covered;
  removing the last entry from `knownBaselineMissing` and deleting
  the slice + skip block becomes the long-term unblock. Closes
  #246, in #272.

### Docs

- `README.md` — added a top-of-page callout pointing first-time
  readers (especially external AI agents) at `docs/agent-guide.md`.
  Closes #244, in #268.
- `CLAUDE.md` — close-the-loop discipline now explicitly requires
  `docs/agent-guide.md` updates whenever a new top-level command
  lands or an existing one changes meaningfully. The CI anti-rot
  test (above) gates presence; humans gate quality. Closes #248,
  in #270.
- `docs/configuration.md` — substantial restructure. Now leads
  with the project-local `.gaia/config.yaml` layer (the recommended
  pattern for `default_profile` / `default_repo`), covers the
  global layer second as "profile definitions only", and adds a
  new **Multi-project safety** section documenting the
  contamination footgun (a global `default_profile` applies to
  every `gaia` call from any cwd, silently misrouting calls when
  the cwd's git remote points at a different forge). New
  per-field where-to-pin reference table, new repo-resolution
  chain documentation (`--repo` → git-remote autodetect →
  project `default_repo` → error). Closes #275, in #276.
- `docs/auth.md` — corrected precedence wording (was reading
  low-to-high) and added a callout linking to the new
  multi-project safety section in #276.

### Internal

- Post-v0.3.0 housekeeping: `.claude/` worktrees directory added
  to `.gitignore`; `Formula/gaia.rb` bumped to v0.3.0 (the
  Homebrew formula auto-bump pipeline lands in the same release
  via #265, but the v0.3.0 cut predated the fix). In #257.

## [0.3.0] — 2026-05-09

Phase 4 complete. New `gaia actions` command group, cache-backed search,
`--format value` for scriptable scalar extraction, `gaia server version`,
`gaia issue edit --add-label/--remove-label`, and a batch of bug fixes.

### Added

- **`gaia actions list|view|logs|rerun`** — new command group for Forgejo
  Actions workflow runs. `logs` fetches per-job log lines from the ZIP
  archive endpoint; `--failed-only` limits output to failed jobs. MCP tools
  `gaia_actions_list_runs`, `gaia_actions_view_run`,
  `gaia_actions_get_logs`, `gaia_actions_rerun` added. Closes Phase 4
  CI-helpers item. (#183, #228)
- **Cache-backed search** — `gaia search` with `--repo` now scans the local
  SQLite cache first. A warm cache short-circuits the upstream call entirely;
  a cold cache falls through transparently. Cache interface gains `Scan`
  method for bulk payload retrieval. (#43, #229)
- **`GetCached` wired to all remaining single-resource reads** — `GetWikiPage`,
  `GetRelease`, `GetWebhook`, `GetWebhookDelivery`, and `GetPackage` now use
  conditional GET (ETag / If-Modified-Since) and SQLite TTL cache. Write-side
  invalidation added to `CreateWebhook`, `EditWebhook`, `DeleteWebhook`,
  `DeletePackage`, `UploadPackage`. New `kindDelivery` constant and `itoa64`
  helper in both provider cache_keys files. (#153, #227)
- **`gaia server version`** — new subcommand fetches the upstream Forgejo /
  Gitea / GitHub API version. (#222)
- **`gaia issue edit --add-label / --remove-label`** — append or remove
  individual labels without replacing the full label set. (#201, #223)
- **`--format value`** — new output mode extracts a single scalar field from
  any command without needing jq. Trust-tagged fields are automatically
  unwrapped. (#220)
- **`.gaia/chains/watch-and-merge.yaml`** — saved chain that polls a PR until
  all CI checks pass, then merges. (#221)

### Fixed

- `core/forgejo`: `EditWikiPage` now resolves hyphenated page slugs via a
  list-and-match fallback when a direct slug GET returns 404. Forgejo
  canonicalises slugs on creation (e.g. "Quick-Start" → "Quick-Start.-"),
  so a direct PUT would silently create a duplicate. (#178, #226)
- `gaia --version` flag now works; previously cobra returned "unknown flag:
  --version". (#225)
- `gaia issue create --label <name>` resolves label names to IDs before
  posting. Forgejo's API requires integer IDs; sending names caused HTTP 422.
  (#225)
- `gaia release publish` now uses replace semantics: existing same-named
  assets are deleted before re-uploading, preventing duplicates when a CI
  workflow re-runs. (#219)
- Release workflow: GHCR container build no longer fails when
  `docker buildx create` conflicts with an existing builder instance; ephemeral
  runner credential warning suppressed. (#210)

## [0.2.8] — 2026-05-07

Release chain hardening and goreleaser fix — first fully end-to-end clean release.

### Fixed

- `core/forgejo`: per-item CI check states were always empty because
  `apiStatusItem` mapped `json:"state"` but Forgejo sends `"status"` for
  individual status entries. This caused `gaia pr ci-wait` to exit 0
  (success) even when checks had failed, since `classifyChecks` short-circuits
  on `Failed == 0`. (#196)
- `.gaia/chains/release.yaml`: `await-workflow` step now resolves the
  annotated tag to its commit SHA (`git rev-parse "$TAG^{}"`) before polling
  `gaia pr ci-wait --ref`. Forgejo's `/commits/{ref}/status` endpoint does not
  resolve tag names — polling by tag name always returned empty state and timed
  out. (#204)
- `.gaia/chains/release.yaml`: validate step used `${TAG#v}` bash parameter
  expansion in a shell comment; the chain runner expands all `${...}` patterns
  before passing the script to the shell, so the comment itself triggered
  "unresolved variable references". Replaced with `sed 's/^v//'` and reworded
  the comment. (#203)
- `.goreleaser.yml`: added explicit `url_template` to the `brews:` block.
  Without it, goreleaser v2 tries to derive the formula download URL from the
  release record — but since the workflow runs with `--skip=publish`, the
  release is disabled and goreleaser fails with "release is disabled, cannot
  use default url_template", blocking all downstream steps. (#205)

## [0.2.7] — 2026-05-06

Release workflow fixes, chain dogfooding, and release automation.

### Added

- `.gaia/chains/release.yaml` — saved release chain that runs pre-flight
  validation, quality gate, tags and pushes, then polls the release workflow
  via `gaia pr ci-wait --ref <tag>`, and verifies the resulting release
  record. Fully autonomous — no yield/human step needed. (#192, #193, #194)
- `gaia pr ci-wait --ref <ref>` — new flag polls commit status for any git
  ref (tag, branch, or SHA) without needing a PR number. Consistent across
  Forgejo and GitHub (both accept tag names in the commit-status endpoint).
  Also fixes a pre-existing bug where an empty state ("") was treated as
  success instead of pending. (#194, #195)
- Release workflow: `latest` git tag now moves to each new release tag on
  every successful publish. Enables `TAG=latest` in install scripts and
  `:latest` docker images. (#191)
- Release workflow: README version badge is automatically committed and pushed
  to main after each successful release. (#191)

### Fixed

- `core/config/resolve.go`: `--provider github` no longer bleeds through the
  Forgejo profile's `api_url`, fixing GitHub publish hitting the wrong host.
  (#189)
- `core/github/releases.go`: GitHub asset uploads now set `Content-Length`
  explicitly; previously `uploads.github.com` rejected all uploads with HTTP
  400 "Bad Content-Length". Adds `size int64` parameter to
  `provider.UploadReleaseAsset`. (#190)
- `.goreleaser.yml`: fixed v2 field names (`ids`→`builds`,
  `formats`→`format`). (#187, #188)
- Release workflow: stderr capture files moved to `$RUNNER_TEMP` so goreleaser
  no longer sees a dirty working tree. (#188)
- Release workflow: `GORELEASER_TAP_DEPLOY_KEY` placeholder written to env
  when the secret is absent, preventing template evaluation failure when
  `--skip=homebrew` is active. (#187)

### Added (earlier in 0.2.x)

- `scripts/install.sh` — one-line installer for the prebuilt
  `gaia` + `gaia-mcp` binaries. Detects OS/arch, sha256-verifies
  before installing, idempotently wires `$PREFIX` into the user's
  shell rc (bash/zsh/fish), and honours `GITEA_TOKEN` /
  `FORGEJO_TOKEN` / `GAIA_TOKEN` for auth-gated forges. Works
  via `curl ... | bash` from the canonical Forgejo URL. (#174)

## [0.2.0] — 2026-05-03

Second tagged release. **Phase 4 mostly shipped**, **chain epic
complete (phases A through C)**, **substantial security hardening
pass**, and the **distribution infrastructure** that v0.1.0 set up
is now operationally proven. 158 commits across ~30 PRs since 0.1.0.

This is a substantive minor bump — multiple wire-shape changes (see
"Wire-shape changes" below), the Go floor moved from 1.23 to 1.25,
and `mcp-go` jumped 18 versions. Per the 0.x carve-out, breakage at
the public surface is allowed at minor bumps until 1.0.0.

### Added — chain primitives (closes Phase A through C of #112)

- **Phase A** (#116): linear chains with shell-style variable
  substitution, `on_failure` blocks, `--dry-run`, fail-fast
  semantics. `gaia chain run --chain-file ...`.
- **Phase B-1** (#117): yield/resume primitive with disk-backed
  state at `~/.local/state/gaia/chains/<token>.yaml`. Fixed
  yield-condition vocabulary (`auth_error`, `not_found`,
  `rate_limited`, `timeout`, `unknown_error`) mapped from gaia's
  exit codes. `gaia chain resume <token>`, `gaia chain list`,
  `gaia chain abort <token>`.
- **Phase B-2** (#131): per-step `timeout` + `retry` (with
  exponential / linear / constant backoff), chain-level
  `default_yield_on`, `cleanup:` block on abort,
  `gaia chain resume --decision modify --modify-step ID
  --modify-vars k=v`.
- **Phase B-3** (#144): saved chains under `.gaia/chains/<name>.yaml`
  (project-local) → `~/.config/gaia/chains/<name>.yaml` (global);
  `gaia chain run <name>` resolves both. New CI-aware exit codes
  7–11 (`MergeConflict`, `ReviewRequired`, `PolicyViolation`,
  `CheckFailed`, `CheckFlaky`) wired through `MapExitCode`. New
  `gaia pr ci-wait` polling subcommand. Canned `pr-create-and-land`
  chain ships in `.gaia/chains/`. **Measured 81% byte reduction
  and 7→1 tool-turn reduction** vs the equivalent multi-call agent
  flow.
- **Phase C** (#155): `parallel:` blocks with `max_concurrent` +
  `fail_fast`, `for_each:` iteration (serial or parallel),
  `chain:` composition (saved-chain dispatch as a step), nested
  yield/resume across composed chains, recursion + cycle
  detection. **Measured 80% wall-clock reduction** on parallel
  fan-out.

Closes the chain primitive's design horizon for v0.x. Phase D
ideas are separate issues if and when they surface.

### Added — Phase 4 features (#4 epic, partial)

- **SQLite cache** (#42, #152): conditional GET via stored ETag
  and Last-Modified; per-host cache file at
  `~/.cache/gaia/<provider>/<host>.db`; TTL strategy distinct for
  single-resource reads (5min) vs lists (30s); per-bearer safety
  for the HTTP MCP transport (shared cache, never populated using
  another tenant's bearer); `gaia cache nuke` and `--no-cache`
  global flag. Measured ~820× speedup on `make cache-bench` with
  100×issue-view loop.
- **Cache architecture decoupled** (#158, #164): `core/cache` is
  now interface-only; `core/cache/sqlite/` holds the
  `modernc.org/sqlite`-backed implementation. Five binary-side
  packages stopped pulling SQLite into their test compile;
  `core/forgejo` test build dropped 40%.
- **NDJSON streaming output** (#46, #151): `--format ndjson` for
  every list-style command (issues, PRs, labels, releases, wikis,
  packages, webhooks, search, comments). Per-line `_trust=external`
  preservation; broken-pipe cancellation propagates upstream so
  agents that read partial results stop fetching subsequent pages.
  Measured ~150× faster time-to-first-byte on a 100-issue list.
- **Webhooks** (#85, #143): `gaia webhook list / view / create /
  edit / delete / deliveries / redeliver / test`. Closes the last
  "must use curl" gap from CLAUDE.md's pre-v0.2 dogfood contract.
  Delivery list is summary-shape; full bodies via
  `gaia webhook deliveries <id> --get N` (~16× smaller list
  responses).
- **Packages** (#107, #123, #122, #130): `gaia packages list /
  view / delete / upload`. Forgejo provider full implementation;
  GitHub provider stub returns NotImplemented (per-registry
  publish dispatch is a follow-up). Generic-registry upload via
  `PUT /packages/{owner}/generic/{n}/{v}/{file}`.
- **Wikis** (#108, #124, #120, #129): `gaia wiki list / view /
  search / edit / delete`. Forgejo via REST API; GitHub via local
  clone cache at `~/.cache/gaia/wikis/{owner}/{repo}/`. Wiki
  search is the headline agent-cost win — one structured response
  with per-hit snippets vs N WebFetches; ~25× smaller than the
  equivalent agent loop.
- **`gaia release publish`** (#111, #115): cross-forge release
  orchestration — get-or-create release record + multi-asset
  upload; replaces the curl boilerplate in
  `.forgejo/workflows/release.yml`.

### Added — distribution infrastructure (#5 epic, partial)

- **GitHub mirror prep** (#47, #133): operator runbook +
  one-shot `scripts/mirror-to-github.sh` + optional auto-mirror
  workflow gated on `GITHUB_MIRROR_SSH_KEY` secret.
- **Homebrew tap** (#49, #134): `Formula/gaia.rb` shipped;
  goreleaser `brews:` block auto-updates the formula on every
  tagged release via `GORELEASER_TAP_DEPLOY_KEY`.
- **Release workflow polish** (#50, #141): tag-only triggering
  with `git merge-base --is-ancestor` enforcement; per-step
  stderr re-emitted as `::error::` annotations; mirror tag-push
  gated on the SSH-key secret being present;
  `scripts/cut-release.sh` operator one-liner that validates,
  tags, and pushes.

### Added — testing + CI infrastructure

- **CLI golden-file harness** (#118, #128): `cmd/gaia/main_test.go`
  drives `cli.NewRootCmd()` in-process against a fake forge HTTP
  fixture; `cmd/gaia/testdata/<command>/<scenario>/scenario.yaml`
  + `.golden` files capture the wire shape end-to-end.
- **Harness backfill** (#127, #145): 60 scenarios across
  issue/pr/label/release/wiki/packages. `internal/cli` total
  coverage 52.8% → 75.9%.
- **CI coverage summary as PR comment** (#119, #125): every PR
  gets the `go tool cover -func` output posted as a marker-tagged
  comment so reviewers see drift without clicking into the run.
- **CI third-party action SHA pinning** + **govulncheck gate**
  (#140 parts 2 + 3): see "Security" below.

### Changed

- **`github.com/mark3labs/mcp-go`** bumped from v0.32.0 to v0.50.0
  (#138). Closes a 10-month / 18-version stale-dependency window;
  mcp-go v0.50.x carries hardening of its streamable-HTTP
  transport and accumulated bug fixes that the old pin would have
  shipped as zero-days for any embargoed advisory landing in the
  gap. Side effect: mcp-go v0.50.x requires Go ≥ 1.25.5, so
  gaia's go.mod floor moved from 1.23 to 1.25. CI workflows, the
  Dockerfile build base, and the install / release docs all
  bumped together. Drop-in API compat — no source-level
  adjustments to gaia-mcp were needed.
- **golangci-lint** v1.64.8 → v2.12.1 (forced by the Go bump;
  v1 lint refuses configs targeting newer toolchains). Config
  migrated to v2 schema. Five staticcheck rules
  (`QF1001/QF1002/QF1011/ST1000/ST1021`) disabled to keep parity
  with the v1 ruleset; pre-existing violations are tracked as
  follow-up cleanup.
- **`docs/dogfood-comparison.md`** restructured (#157): grew from
  evidence doc into merge-conflict generator. Now a short curated
  summary; per-resource measured baselines moved to
  `bench/dogfood-<resource>.md` (one file per resource, no shared
  append point).

### Security

- **Chain runner: shell-injection via vars and captures closed**
  (#135, #147). Substituted variable values are now shell-quoted
  before insertion into the `sh -c` command; hostile vars or
  captured forge content can no longer inject shell metacharacters.
  Existing chains that wrapped `${var}` references in their own
  `"..."` or used `${var}` as multi-token shell input need to drop
  the manual quoting (the shell-quoting is now automatic) or wrap
  with `sh -c "${var}"` in their own step. Bundled chain scenarios
  and `docs/chain.md` example are updated.
- **Chain runner: child env scrubbed** (#140 part 4, #147). Allowlist:
  `PATH, HOME, USER, LOGNAME, LANG, LC_ALL, TERM`. Forge tokens
  (`GITEA_TOKEN`, `FORGEJO_TOKEN`, `GH_TOKEN`, `GITHUB_TOKEN`),
  cloud credentials (`AWS_*`, `GCP_*`, `AZURE_*`), and any other
  operator-scope vars are stripped. Combined with the shell-quoting
  above, this closes the "hostile forge response → shell-injection
  → `env` reads my forge token" exfiltration path even if a future
  shell-injection regression sneaks past review. See `docs/chain.md`
  "Security: env scrubbing" for the rationale and the design
  contract.
- **GitHub wiki cache: path-traversal closed** (#136, #147).
  Validates owner/repo/slug against an allowlist
  (`[A-Za-z0-9_.-]+` minus `.`, `..`, hidden-name prefixes,
  separators, null bytes) before joining into filesystem paths;
  `filepath.Rel` post-condition catches any escape that slips
  past the allowlist.
- **GitHub wiki cache: token-in-argv closed** (#137, #147). Git auth
  header now travels via `GIT_CONFIG_*` env vars instead of in the
  URL passed to argv; tokens are no longer visible in `ps`,
  `/proc/<pid>/cmdline`, or process accounting. Requires git 2.31+
  for `GIT_CONFIG_COUNT` support. `runGit` error formatting also
  scrubs both the joined argv and combined output through
  `scrubToken` as defence in depth.
- **MCP envelopes / CLI output: indirect prompt-injection markers**
  (#146, #147). Fields containing user-provided forge content
  (issue bodies, PR bodies, comments, wiki content, release bodies,
  titles, search snippets, etc.) now carry `_trust: "external"`
  markers in JSON and `<<<EXTERNAL` / `EXTERNAL>>>` delimiters in
  pretty output. `--no-external-markers` opts out of the pretty
  wrapping for tooling that processes raw output; JSON output is
  always tagged. See `docs/agent-guide.md` "Threat model: tool
  results carry untrusted content" for the recommended
  system-prompt snippet.
- **gaia-mcp `/readyz` no longer drains forge rate limit** (#139,
  #147). The endpoint is now liveness-equivalent (200 "ready"
  while the listener is bound, no upstream call). Operators who
  need to monitor forge reachability should run a CLI probe
  (`gaia whoami`) from their monitoring host — real MCP callers
  already see upstream errors on their own `/mcp` requests.
- **`govulncheck` gate** (#140 part 3, #150). CI now runs
  `govulncheck ./...` on every PR and push to main. Known CVEs in
  transitive deps that touch reachable code paths gate the merge.
  Initial pass: clean against the Go 1.25.9 stdlib + current deps;
  the floor was bumped from 1.25.5 (mcp-go's required minimum) to
  1.25.9 via a `toolchain` directive to clear 7 stdlib CVEs the
  older patch carried.
- **CI third-party actions pinned by SHA** (#140 part 2, #150).
  A compromised actions/* org can no longer push a malicious
  update under a tag we already trust. Applied across
  `.forgejo/workflows/*.yml` and `.github/workflows/ci.yml`. See
  each workflow header for the bump discipline.
- **`SECURITY.md`** added at the repo root (#140 part 1, #150).
  Documents threat model, reporting channel
  (`aidev@stewartbrothers.com.au`), and 7-day-ack / 30–90-day-fix
  timeline.

### Fixed

- **JSON declaration order preserved** in trust-tagged output
  (#148, #150). The reflection-based marshal walker temporarily
  emitted `data.*` field order as alphabetical (a side-effect of
  going through `map[string]any`); now uses an order-preserving
  `json.Marshaler` shim that walks `reflect.StructField`
  declaration order. Restores the pre-#146 wire-shape ordering
  that consumers relying on byte-identical serialisation depend
  on.
- **Chain `--var` preserves commas in values** (#154, #156).
  cobra's `StringSliceVar` was splitting on commas, breaking
  `--var issues_json=[1,2,3]`. Switched to `StringArrayVar`. Same
  fix applied to `chain resume --modify-vars`.
- **Chain `fail_fast` cancellation propagates** (#149 follow-up,
  #167). `parallel: { fail_fast: true }` was advertised as
  "cancels still-running siblings as soon as one fails." On Linux
  it didn't — the default `exec.CommandContext` killed the
  immediate `sh` but orphaned `sleep` children kept running.
  Fixed by placing each `sh -c …` child in its own process group
  (`Setpgid: true`) and killing the negative PID on cancellation.
- **`make cover` covdata invocation scoped to test-bearing
  packages** (#165, #169). Go 1.25.x's auto-downloaded toolchain
  ships without `covdata` (~7 tools vs the full install's 18);
  `go test ./...` failed three times trying to invoke covdata for
  no-test packages, even though every test passed. `make cover`
  now filters to packages with `TestGoFiles`/`XTestGoFiles`
  before invoking go test, avoiding the missing-tool path
  entirely. CI was unaffected (actions/setup-go installs a
  complete toolchain); local devs see green again.
- **Harness `duration_ms` always emitted** (#132). The chain
  golden-file harness's `normalize` step rewrites any
  `duration_ms` value to 0, but `omitempty` on the int64 field
  was stripping the field entirely on Linux when fast `echo`
  steps measured 0ms. Mac never hit zero locally, so the bug
  silently failed Linux CI on every PR. Dropped `omitempty` on
  the `Result.DurationMs` and `StepResult.DurationMs` fields.
- **Release workflow creates Forgejo release if it doesn't exist**
  (#110, #114). Forgejo doesn't auto-create on tag push (unlike
  GitHub); workflow now does `get-or-create` before uploading
  artifacts.
- **`auth.ProjectRoot` recognises `.git` worktree pointers**
  (#142, #144). Previously only treated `.git` directories as
  project markers; now treats `.git` files (worktree pointers
  + submodules) the same way. Was silently breaking project-local
  saved-chain resolution inside worktrees.
- **#150 hygiene bundle's chain Phase B-2 absorption** (#131).
  Coordinated rebase across the security pass + B-2 work; chain
  state-leak fix from PR #117 (always-delete-old-token in
  `Resume`) re-verified after the rebase dance.

### Wire-shape changes (consumers must update)

- **Trust-tagged fields** now serialise as
  `{"_trust":"external","_value":"<text>"}` rather than as a bare
  string. Consumers decoding gaia JSON into typed structs need
  to update their decode shapes — see the in-tree test pattern
  in `internal/cli/issue_test.go` (`trustExternal` helper).
- **JSON field order**: temporarily shifted to alphabetical when
  the trust marshal walker landed in #146; **restored to
  struct-declaration order in #148**. Consumers that re-baselined
  for the alphabetical interim should re-baseline once more
  against declaration order. This was the only ordering churn
  in the cycle.
- **New exit codes 7–11**: `MergeConflict (7)`, `ReviewRequired
  (8)`, `PolicyViolation (9)`, `CheckFailed (10)`, `CheckFlaky
  (11)`. Network is at 6 (unchanged). Agents that branched on a
  generic "non-zero exit = failure" continue to work; agents
  that want fine-grained recovery should map per the new table.
  See `docs/exit-codes.md`.
- **`StepResult.DurationMs` and `Result.DurationMs`** are no
  longer `omitempty`. A 0ms step now serialises as
  `"duration_ms": 0` rather than omitting the field. Decoders
  that expected the field to sometimes be missing need to handle
  the explicit-zero shape.
- **`Invalidator` API changed** (cache decouple): `(*Cache).Invalidator()`
  → `cache.NewInvalidator(c)` free function. The `Cache` type is
  now an interface; the SQLite-backed implementation lives in
  `core/cache/sqlite/`. Callers that imported `core/cache.Cache`
  as a concrete type need to adopt the interface.

### Phase 4 work deferred to v0.3.0

- **#43** Cross-resource indexed search (depends on the cache
  layer that just landed; clean follow-up).
- **#45** `gaia ci runs` / `gaia ci logs` helpers.
- **#51** Forgejo upstream submission (process / outreach work,
  not feature work).
- **#153** Wire remaining `Get<Resource>` methods through
  `GetCached` (cache wiring follow-up; today only `GetIssue` and
  `GetPullRequest` are cached).

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

[Unreleased]: https://github.com/stewartbrothers/gaia/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/stewartbrothers/gaia/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/stewartbrothers/gaia/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/stewartbrothers/gaia/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/stewartbrothers/gaia/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/stewartbrothers/gaia/releases/tag/v0.2.0
[0.1.0]: https://github.com/stewartbrothers/gaia/releases/tag/v0.1.0
