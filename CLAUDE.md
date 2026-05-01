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

4. **Keep the gate green every commit.** `make vet test build` must succeed
   locally before `git commit`. Once #7 lands, `make lint` joins the gate.
   CI (`.forgejo/workflows/ci.yml`, mirrored to `.github/workflows/ci.yml`)
   enforces the same.

5. **Commit messages lead with the *why*.** Imperative subject, blank line,
   body explaining motivation, then `Closes #N` / `Refs #N`, then the
   `Co-Authored-By: Claude ... <noreply@anthropic.com>` trailer for
   AI-generated commits.

6. **One logical change per commit.** If a PR carries multiple logical
   changes, split them into multiple commits — don't squash unrelated work.

7. **Never force-push without rebasing against `main` first**, and only on
   feature branches. Force-push to `main` is forbidden.

## Build / test / lint

```bash
make build     # → bin/gaia, bin/gaia-mcp
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt -s -w .
make lint      # golangci-lint (install: https://golangci-lint.run/)
make tidy      # go mod tidy
make clean
```

## Forge access (issues, PRs, releases)

This repo is hosted on a self-hosted Forgejo instance with two DNS names —
they are the same instance:

- **HTTPS API base:** `https://your-forge.example.com/api/v1`
- **SSH (push/pull):** `git@github.com:stewartbrothers/gaia.git`
- **Repo slug:** `Gerwood/gaia`
- **Auth:** token in env `GIT_FORGE_GITEA_TOKEN`, sent as
  `Authorization: token <token>`. Forgejo's API is Gitea-compatible.

API surface used routinely:

- `GET  /repos/{owner}/{repo}/issues?state=all&type=issues` — list issues
- `POST /repos/{owner}/{repo}/issues` — create
- `PATCH /repos/{owner}/{repo}/issues/{n}` — update body/labels/state
- `POST /repos/{owner}/{repo}/pulls` — open PR
- `GET  /repos/{owner}/{repo}/pulls/{n}.diff` — raw diff
- `GET  /repos/{owner}/{repo}/labels` — list labels
- `POST /repos/{owner}/{repo}/labels` — create label

**Never echo or log `$GIT_FORGE_GITEA_TOKEN`.** Pipe it directly into curl as
a header; don't store it in shell history-visible variables, don't write it
to disk, don't include it in commit messages or PR bodies.

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
