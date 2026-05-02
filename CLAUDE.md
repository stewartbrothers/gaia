# gaia — guidance for human and AI contributors

`gaia` (Git AI Access) is a Go module providing a token-trimmed CLI and an MCP
server (stdio in Phase 1; HTTP/SSE in Phase 3) for Forgejo and — from Phase 2 —
GitHub. The roadmap and active work live in the repo's issue tracker.

## Module + binaries

- **Module path:** `github.com/stewartbrothers/gaia`
- **Binaries:** `gaia` (CLI), `gaia-mcp` (MCP server). Both built from
  `cmd/gaia` and `cmd/gaia-mcp` respectively, sharing logic via `core/`.
- **Go version:** 1.23+

## Workflow rules

These are hard rules. Apply them on every change.

1. **Feature branch per logical unit of work.** `feature/<slug>` → push → PR
   → merge → delete the branch. Never commit work-in-progress directly to
   `main`. The initial scaffold (commit `72a6698`, issue #6) landed on `main`
   as a one-time bootstrap exception; from then on everything goes through a
   PR.

2. **One issue per change.** Every PR closes (or refs) at least one issue.
   Issues drive work, so a session interruption never loses the plan. Use
   `Closes #N` in the commit body to auto-close on merge, or `Refs #N` for a
   partial.

3. **Open PRs via the Forgejo API — do not ask the user to open a compare
   URL.** Pattern (token never echoed):

   ```bash
   curl -sSf -H "Authorization: token $GIT_FORGE_GITEA_TOKEN" \
        -H "Content-Type: application/json" \
        -X POST "https://your-forge.example.com/api/v1/repos/Gerwood/gaia/pulls" \
        -d '{"title":"...","body":"...","head":"feature/<slug>","base":"main"}'
   ```

4. **Keep the gate green every commit.** `make vet lint cover build` must
   succeed locally before `git commit` — `cover` runs the same `go test
   -race -coverprofile=...` invocation CI runs and prints the per-function
   summary; `lint` runs `golangci-lint` against `.golangci.yml`. CI
   (`.forgejo/workflows/ci.yml`, mirrored to `.github/workflows/ci.yml`)
   enforces the same on every PR (including every push to an open PR) and
   every push to `main`. The lint version pinned in CI must match what's
   used locally — bump both at once.

5. **Commit messages lead with the *why*.** Imperative subject, blank line,
   body explaining motivation, then `Closes #N` / `Refs #N`, then the
   `Co-Authored-By: Claude ... <noreply@anthropic.com>` trailer for
   AI-generated commits.

6. **One logical change per commit.** If a PR carries multiple logical
   changes, split them into multiple commits — don't squash unrelated work.

7. **Never force-push without rebasing against `main` first**, and only on
   feature branches. Force-push to `main` is forbidden.

## Testing discipline

### TDD is the default

Write the failing test first, then the minimum implementation that makes
it pass. Every code change in `core/`, `cmd/`, and `internal/` ships with
the test that justified it. Refactors don't need new tests, but the
existing tests must stay green.

Specific application:

- **Provider methods** — every method on `core.Provider` is paired with
  `httptest`-backed unit tests in the same package. A new method doesn't
  merge until it has both happy-path coverage and at least two
  error-path tests (e.g., 404 → `NotFound`, 401 → `Auth`).
- **CLI subcommands** — end-to-end tests live in `cmd/gaia/testdata/` as
  golden files driven by an in-process fake forge server. New subcommand
  → new golden file.
- **MCP tools** — each registered tool has a smoke test that calls
  `tools/call` over an in-process MCP harness.
- **Bug fixes** — write the regression test first, watch it fail, then
  fix. The test is the proof the fix doesn't decay later.

If a fix is small enough that writing the test takes longer than the
fix itself, write the test first anyway.

### Code coverage

CI runs `go test ./... -race -count=1 -covermode=atomic
-coverprofile=coverage.out` on every PR and every push to `main`, then
prints the per-function coverage summary via `go tool cover -func=...`
to the job log. The summary is the contract — read it on PR review.

Locally:

- `make cover` — runs the suite with coverage, prints the summary.
- `make cover-html` — produces `coverage.html` for browser inspection.
- `make test-race` — race suite without coverage, for fast iteration.

**Never exclude files from coverage measurement to hit a target.** If
coverage drops, the response is to write the missing tests, not to hide
code from the measurement. A coverage threshold gate may be added once
#23, #24, #28, and #29 land — at that point we'll set a starting
threshold based on what the suite actually achieves and ratchet from
there. Until then the summary is informational, but every test issue is
expected to move the per-package number up, not down.

## Build / test / lint

```bash
make build       # → bin/gaia, bin/gaia-mcp
make test        # go test ./...
make test-race   # go test ./... -race -count=1
make cover       # test-race + per-function coverage summary
make cover-html  # like `make cover`, plus coverage.html
make vet         # go vet ./...
make fmt         # gofmt -s -w .
make lint        # golangci-lint (install: https://golangci-lint.run/)
make tidy        # go mod tidy
make clean
```

## Forge access (issues, PRs, releases)

This repo is hosted on a self-hosted Forgejo instance with two DNS names —
they are the same instance:

- **HTTPS API base:** `https://your-forge.example.com/api/v1`
- **SSH (push/pull):** `git@github.com:stewartbrothers/gaia.git`
- **Repo slug:** `Gerwood/gaia`
- **Auth env var:** `GITEA_TOKEN` (the community-standard name used
  by `tea` and every Gitea/Forgejo guide). gaia also honors
  `FORGEJO_TOKEN` (canonical) and a profile-pinned `token_env`. The
  project previously used `GIT_FORGE_GITEA_TOKEN` as a unique-prefix
  workaround — that name is **no longer the project convention**;
  set `export GITEA_TOKEN=...` once instead. Sent as
  `Authorization: token <token>` for raw curl. (For GitHub: `GH_TOKEN`
  or `GITHUB_TOKEN`.)
- **Best path:** run `gaia auth forgejo https://your-forge.example.com/api/v1`
  once. The credential lands in `~/.config/gaia/credentials.yaml`
  and no env vars are needed thereafter.

### Project-local config: `.gaia/config.yaml`

This repo ships a `.gaia/config.yaml` that pins the provider, API
URL, and default repo for every `gaia` invocation inside this
checkout. Effect: `gaia issue list`, `gaia pr create ...`,
`gaia whoami` all work **bare** — no `--provider`, no `--api-url`,
no `--repo`, no env-var prefix beyond the token.

  $ cat .gaia/config.yaml
  default_profile: stewartbrothers
  default_repo: Gerwood/gaia
  profiles:
    stewartbrothers:
      provider: forgejo
      api_url: https://your-forge.example.com/api/v1

  $ gaia whoami           # works
  $ gaia issue list       # works
  $ gaia pr create ...    # works

The file is committable — no secrets, just non-secret defaults.
Layering order: project (.gaia/config.yaml) > global
(~/.config/gaia/config.yaml) > env > flags. See `core/config/`
for the merge logic.

### Dogfood: use `gaia` for forge ops gaia supports

When writing a script, agent prompt, or one-off shell snippet that needs to
hit the forge, **call `gaia` instead of raw `curl` (or `tea`/`gh`)**. This
applies to *both* read and write paths — gaia covers nearly the full
useful surface today.

Current coverage (pass `--format json --fields a,b,c` to keep output
tight when scripting):

- Identity: `gaia version`, `gaia whoami`, `gaia auth forgejo|gh|status|logout`
- Issues: `list | view | create | edit | close | reopen | comment | comment-edit | comment-delete`
- PRs: `list | view | diff | comments | create | edit | close | reopen | comment-create | merge | review | checkout`
- Labels: `list | create | edit | delete`
- Releases: `list | view | create | edit | delete`
- Search: `gaia search <query>`

For read paths, gaia is consistently smaller (often 5–25×, sometimes
~400× with `--fields`) than the raw API response — see
`docs/dogfood-comparison.md` for the per-command numbers.

**Falling back to curl is acceptable only for operations gaia cannot do
yet** — currently:

- **Webhook config** (#85, #44): create/list/edit/delete repo webhooks,
  fetch delivery history, redeliver. Filed as Phase 4 work.

That's it. PR creation, issue creation, comment posting, label
management, and release management all have first-class gaia commands
now — using curl for those is a regression to be flagged in review.

When you DO need curl for a real gap, append a line to the usage log
at `.gaia-usage.jsonl` (repo-adjacent, gitignored) with
`kind: forge_op`, `tool: curl`, and a `curl_reason` field naming the
gaia gap. The log is for **analysis** (which commands matter, where
the gaps hurt) — it is **not** an action tracker.

### Bugs become issues, common-path bugs get priority fixes

When you encounter a bug in gaia or anywhere in the project, **file
it as an issue immediately** via `gaia issue create`. Don't queue it
in the usage log, don't note it in conversation, don't promise to
"address it later" — the issue is the action tracker, the log is
just evidence of how often the bug bites.

Classify by frequency:

- **Common-path bug** (hits on every PR open, every tool call,
  every commit): **priority fix.** Interrupt current work to fix
  it. Example: #102 — gaia not honoring `GITEA_TOKEN` as a fallback
  for the Forgejo provider. Hit on every gaia call until fixed.
- **Edge-case bug** (rare paths, recovery scenarios): file it,
  prioritize normally.

Common-path bugs are the ones that erode dogfood confidence
fastest — every workaround compounds. Filing + fixing immediately
is cheaper than the cumulative cost of "I'll just add the env-var
prefix one more time."

When a new gaia command lands, **update `docs/dogfood-comparison.md`**
with a row showing gaia bytes vs. the closest curl/tea equivalent. The
script at `scripts/dogfood-compare.sh` produces the numbers; the point is
to keep evidence that gaia is shrinking output, not just claiming it.

### Raw API for gaps

The endpoints used by the curl-still-needed paths:

- `POST /repos/{owner}/{repo}/issues` — create
- `PATCH /repos/{owner}/{repo}/issues/{n}` — update body/labels/state
- `POST /repos/{owner}/{repo}/pulls` — open PR
- `POST /repos/{owner}/{repo}/labels` — create label
- `PATCH /repos/{owner}/{repo}/issues/comments/{id}` — edit a comment

**Never echo or log `$GITEA_TOKEN`** (or `$FORGEJO_TOKEN`, or any
PAT). Pipe it directly into curl as a header; don't store it in
shell history-visible variables, don't write it to disk, don't
include it in commit messages or PR bodies.

## Roadmap (tracking epics)

| Phase | Tracker | Goal |
| ----- | ------- | ---- |
| 1     | #1 | Forgejo provider, full CLI surface, stdio MCP server |
| 2     | #2 | GitHub provider parity |
| 3     | #3 | Remote MCP transport (HTTP/SSE) |
| 4     | #4 | Cache, indexed search, webhook + CI helpers |
| —     | #5 | Distribution & upstreaming |

Each epic body has a checklist of its child issues. Phase ordering is binding
— don't build ahead. If a Phase 2+ idea lands during Phase 1 work, file an
issue against the relevant epic instead of implementing it.

## Architecture invariants

- **`core/`** is the contract. Both `cmd/gaia` and `cmd/gaia-mcp` are thin
  wrappers over `core.Provider`. Adding a new operation means adding it to
  the `Provider` interface first, then exposing it through both frontends.
- **Trimmed types only.** `core/types` deliberately omits URLs, avatar
  links, internal IDs, and other API bloat. If a downstream consumer needs
  one of those, that's a separate, justified design decision — don't add
  them by reflex.
- **Output envelope is stable and versioned.** See `docs/output-format.md`
  (lands with #5) — `{schema_version, data, _truncated?, _next_cursor?}`.
- **Exit codes are documented and respected.** See `docs/exit-codes.md`
  (lands with #6) — agents branch on them.

## Secrets

Never commit:

- `.env`, `.env.*`
- `*.token`, `*_secret.json`, `*.pem`
- Anything matching `*.local.yaml`

The root `.gitignore` covers these patterns; don't circumvent it.

## When to extend this file

Add a section here whenever you discover a rule, convention, or gotcha that
a future agent would need to re-learn from scratch otherwise. Keep entries
short and actionable. If a rule applies across multiple sibling projects on
this forge, consider also adding it to those projects' CLAUDE.md files.
