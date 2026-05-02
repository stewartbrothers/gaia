# Provider parity

`core/provider.Provider` defines a single interface; the Forgejo and
GitHub providers each implement it against their own forge. This page
documents what's covered, what's intentionally different, and what's
known-imperfect — so callers don't have to reverse-engineer the
provider source to know what to expect.

## Status legend

- **✓ full** — implemented + tested + behavior matches the contract
  documented on the interface method.
- **△ partial** — implemented but with a documented gap (e.g.,
  semantically-different field is silently dropped, or the client-
  side filter has a known edge case).
- **× missing** — interface method is part of the Provider contract
  but the forge implementation hasn't landed yet.
- **n/a** — not applicable to this forge.

## Method × forge

| Method | Forgejo | GitHub | Notes |
|---|---|---|---|
| `Whoami` | ✓ | ✓ | Same shape; `GET /user` on both. |
| `ListIssues` | ✓ | △ | GitHub returns issues+PRs from `/issues`; we filter PRs client-side via `pull_request` field. GitHub's `/issues` does not accept a free-text `q` param — `opts.Query` is silently dropped on GitHub. Use `Search` for query. |
| `GetIssue` | ✓ | ✓ | `WithComments` inlines via the same `/issues/{n}/comments` endpoint on both. |
| `ListPullRequests` | ✓ | △ | GitHub's `/pulls` does NOT accept a `labels` filter; `opts.Labels` silently dropped. Use `Search` to label-filter PRs. |
| `GetPullRequest` | ✓ | ✓ | `WithCISummary`: Forgejo uses `/commits/{sha}/status` (one rolled-up state); GitHub uses `/commits/{sha}/check-runs` (per-check status + conclusion, rolled up client-side). Both produce the same `types.CISummary` shape. |
| `GetPullRequestDiff` | ✓ | ✓ | Same parsed `[]DiffFile` shape. Forgejo uses `.diff` URL suffix; GitHub uses the same path with `Accept: application/vnd.github.v3.diff`. Parser shared via `core/diff`. |
| `ListComments` | ✓ | ✓ | Three-endpoint merge (issue + review + inline) on both. Empty-body reviews dropped from the unified stream. |
| `Search` | ✓ | △ | Different endpoints + shapes. Forgejo: `/repos/issues/search` (or repo-scoped `/repos/{o}/{r}/issues`), bare-array result. GitHub: `/search/issues` with a `{total_count, items}` wrapper; opts.Repo + opts.Kinds fold into the GitHub `q` qualifier (`repo:owner/name`, `is:issue`/`is:pr`). User queries pass through verbatim, so power-users can supply arbitrary GitHub search qualifiers (`label:bug state:closed`, etc.). |
| `CreateIssue` | ✓ | ✓ | Same `POST /repos/{o}/{r}/issues` body on both. |
| `EditIssue` | ✓ | ✓ | `omitempty` on title/body/state/assignees on both. AddLabels/RemoveLabels not yet plumbed in either — label list mutation is a follow-up. |
| `CreateIssueComment` | ✓ | ✓ | Same endpoint family; same `{body}` body shape. |
| `EditIssueComment` | ✓ | ✓ | `PATCH /issues/comments/{id}` on both. |
| `DeleteIssueComment` | ✓ | ✓ | `DELETE /issues/comments/{id}`; 204 success. |
| `CreatePullRequest` | ✓ | ✓ | `POST /repos/{o}/{r}/pulls`. Both accept `{title, head, base, body, draft, labels}`. |
| `EditPullRequest` | ✓ | ✓ | `PATCH`. `Draft` is `*bool` so explicitly setting `false` works (flip a draft back to ready). |
| `MergePullRequest` | ✓ | △ | Merge body field names differ. Forgejo: `do`/`MergeTitleField`/`MergeMessageField`/`delete_branch_after_merge`. GitHub: `merge_method`/`commit_title`/`commit_message`. **DeleteBranch is dropped on GitHub** — GitHub's merge endpoint doesn't accept it; the head ref deletion is a separate `/git/refs` DELETE. Tracked as a Phase 2 follow-up. |
| `ListLabels` | ✓ | ✓ | Returns Name + Color + Description on both. |
| `CreateLabel` | ✓ | ✓ | Hex color (no leading `#`); description optional. |
| `EditLabel` | ✓ | ✓ | Forgejo: list-then-PATCH-by-ID (the upstream takes ID). GitHub: PATCH-by-name directly. Same callable contract; one hop on GitHub, two on Forgejo. |
| `DeleteLabel` | ✓ | ✓ | Same contract as Edit. |
| `SubmitReview` | ✓ | △ | Body/state map identically. Inline-comment shape differs: Forgejo uses `new_position` (line in new file); GitHub uses `position` (position in the diff). `provider.ReviewInlineComment.Line` maps to whichever the active forge expects. GitHub's newer `line` + `side` API for left-side commenting is a follow-up. |
| `ListReleases` | ✓ | ✓ | `GET /repos/{o}/{r}/releases` on both. Pagination via the forge-specific param (`limit` vs `per_page`); same `[]Release` shape on the way out. |
| `GetRelease` | ✓ | ✓ | `GET /repos/{o}/{r}/releases/tags/{tag}` on both. 404 maps to `exitcode.NotFound` on both. |
| `CreateRelease` | ✓ | ✓ | `POST /repos/{o}/{r}/releases`. Both accept `{tag_name, name, body, target_commitish, draft, prerelease}`. |
| `EditRelease` | ✓ | ✓ | Same two-hop pattern as `EditLabel` — GET tag→ID, then PATCH `/releases/{id}`. Forge endpoints PATCH-by-id only on both forges. `Draft`/`Prerelease` are `*bool` so explicit `false` flips work. |
| `DeleteRelease` | ✓ | ✓ | Two-hop: GET tag→ID, then DELETE `/releases/{id}`. 204 on success. |

## Cross-cutting differences

### Auth header

- **Forgejo**: `Authorization: token <token>` (Gitea convention).
- **GitHub**: `Authorization: Bearer <token>` (post-2022 docs preference; works for both classic + fine-grained PATs).

The HTTP clients diverge only on this line. Retry policy, error
mapping, redaction of token in transport errors are identical.

### Pagination

- **Forgejo**: `?page=N&limit=L`.
- **GitHub**: `?page=N&per_page=L`.

`Page.NextCursor` carries the next page number on both. Truncation
heuristic (`returned == limit`) is the same.

### State reconciliation

Both providers reconcile `state` to `open|closed|merged` at the
boundary so consumers don't have to rebuild merged-vs-closed-without-
merge from `{state, merged_at}`.

### CI summary

GitHub splits "did it finish" (status) from "did it pass" (conclusion);
Forgejo unifies into `state`. Both produce the same
`types.CISummary{State, Total, Successful, Failed, Pending}`.
GitHub-only conclusion values that don't have direct Forgejo
analogues (`skipped`, `neutral`) get rolled into `Successful` (treat
not-failing as success); unknown/null go into `Pending`.

### Diff fetching

- **Forgejo**: `/repos/{o}/{r}/pulls/{n}.diff` (URL suffix selects
  format).
- **GitHub**: `/repos/{o}/{r}/pulls/{n}` with
  `Accept: application/vnd.github.v3.diff` (Accept header selects
  format).

The unified-diff parser at `core/diff` is shared between providers.

## What this guarantees for callers

If your code targets the `core/provider.Provider` interface and uses
the documented option structs, it works against both forges with the
caveats above. The **trim contract** — that responses always come
back as the same `core/types` shapes — is upheld on both. So a CLI
or MCP tool written once works on both.

The places callers do need to know which forge is active:

1. **Search query qualifiers** are forge-specific. Forgejo doesn't
   honor `is:issue`; GitHub does.
2. **`opts.Labels` on `ListPullRequests`** silently no-ops on GitHub.
   Use `Search` instead.
3. **`opts.Query` on `ListIssues`** silently no-ops on GitHub. Use
   `Search`.
4. **`MergePullRequestOptions.DeleteBranch`** silently no-ops on
   GitHub. Issue a separate ref delete.

These are documented above and will eventually be addressed in a
Phase 2.5 polish pass.

## How this doc is maintained

When a new method joins the Provider interface, add a row here. When
a forge gets a known-different behavior (different endpoint, different
field name on the wire, gap in capability), update the Notes column.

Treat the matrix as the contract that downstream consumers read. If
something's not documented here as "△" or "×", it's expected to
behave identically across providers.
