# Dogfood note: `gaia issue dep` + `--with-blockers / --with-blocking` (#317)

## What changed

PR 2 of 3 for #317 exposes Forgejo's issue-dependency endpoints
through the CLI surface:

```bash
gaia issue dep list 42                          # blockers (default)
gaia issue dep list 42 --direction blocks       # the inverse view
gaia issue dep add 42 --blocker 7               # "7 blocks 42"
gaia issue dep add 42 --blocks 7                # "42 blocks 7" (inverse)
gaia issue dep remove 42 --blocker 7

gaia issue view 42 --with-blockers 5            # inline N blockers
gaia issue view 42 --with-blocking 5            # inline N blocks
gaia issue view 42 --with-blockers 5 --with-blocking 5
```

Mirrors the agent-redirects-humans pattern from `--with-comments` —
the inline flags trade one extra round-trip each for an
all-in-one-envelope view that an agent can consume without two
shell calls + jq.

## Why bytes-saved isn't the headline

These calls return `[]types.Issue` (the trimmed Issue shape), same
shape `gaia issue list` already returns. Per-record cost identical
to the baseline in `bench/dogfood-baseline.md` (~755 bytes/record).
The win isn't byte-trim, it's:

1. **No URL reconstruction.** Pre-#317 there was no way to ask
   "what blocks issue 42?" via gaia; an agent had to either parse
   the issue body for free-text `Depends on: #N` references (no
   structure) or fall back to raw curl.
2. **One round-trip when inlined.** `gaia issue view 42
   --with-blockers 5` is one shell invocation that issues two HTTP
   calls inside gaia (faster + cached + retried + envelope-shaped).
   The pre-#317 workaround was at least:
   `gaia issue view 42 && curl /repos/.../issues/42/dependencies`
   and hand-merging the outputs.

## Round-trip cost

| Command | Forge calls |
|---|---|
| `gaia issue dep list 42` | 1 GET `/dependencies` |
| `gaia issue dep list 42 --direction blocks` | 1 GET `/blocks` |
| `gaia issue dep add 42 --blocker 7` | 1 POST `/dependencies` |
| `gaia issue dep remove 42 --blocker 7` | 1 DELETE `/dependencies` |
| `gaia issue view 42` | 1 GET issue |
| `gaia issue view 42 --with-blockers 5` | 1 GET issue + 1 GET `/dependencies` |
| `gaia issue view 42 --with-blockers 5 --with-blocking 5` | 1 + 2 = 3 GETs |

Cache (#42) covers the issue GET only; the dep edges aren't cached
(they're write-heavy enough that a fresh read is the right default).

## Provider coverage

- ✅ Forgejo (`core/forgejo/issue_dependencies.go`)
- ❌ GitHub  — NotImplemented stub. GitHub's REST API has no
  equivalent endpoints; GraphQL added `IssueDependency` in 2024 but
  isn't wired. Tracked in #317's open follow-up.

## Out of scope (this PR / v1)

- **Cross-repo dependencies.** Forgejo's API supports a
  `{"index": N, "owner": "o", "repo": "r"}` body for cross-repo
  blockers. CLI surface for that (`--blocker owner/repo#7` syntax,
  cross-fork edge cases) is its own design call.
- **`--with-blockers all` / no-limit shorthand.** Today you pass an
  explicit count. The `--limit` shape is consistent with
  `--with-comments` so muscle memory carries.
- **Pretty rendering of the inlined blockers/blocking lists in
  `gaia issue view --format pretty`.** Currently JSON-only inlines;
  the pretty path shows the issue header but not the inlined lists.
  Filed as a small follow-up.
