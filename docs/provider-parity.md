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
| `ListPackages` | ✓ | △ | Forgejo: `GET /packages/{owner}` with optional `?type=&q=`. GitHub: `GET /users/{owner}/packages` OR `GET /orgs/{owner}/packages` based on owner type (probe via `GET /users/{owner}` to read `type`); supports `?package_type=` filter only — `opts.Q` silently dropped on GitHub. |
| `GetPackage` | ✓ | △ | Forgejo: direct path `/packages/{o}/{type}/{name}/{version}`. GitHub: keys versions by numeric `version_id`, not the human-readable tag. The provider accepts a `version` string and resolves it: pure-integer → version_id directly; otherwise list versions and match against `name` or `metadata.container.tags[]`. The trimmed `Version` field always carries the resolved `name` (e.g., `sha256:…` or semver), not the input. |
| `DeletePackage` | ✓ | △ | Same two-hop resolution as `GetPackage` on GitHub (resolve to version_id, then DELETE). 204 on success on both forges. |
| `ListWikiPages` | ✓ | ✓ | Forgejo: `GET /wiki/pages`. GitHub: scan local clone of `{owner}/{repo}.wiki.git`. See "Wiki backing store" below. |
| `GetWikiPage` | ✓ | ✓ | Forgejo: `GET /wiki/page/{slug}` with base64 body decode. GitHub: read `{slug}.md` from the clone, `git log -1` for last-commit metadata. |
| `SearchWikiPages` | ✓ | ✓ | Both client-side title+body match capped at `MaxPages` (default 100). Forgejo iterates `Get` per page; GitHub scans the clone (no per-page round-trip). Same `[]WikiSearchHit` shape on the way out. |
| `EditWikiPage` | ✓ | ✓ | Forgejo: `POST /wiki/new` (create) or `PATCH /wiki/page/{slug}` (replace). GitHub: refresh clone, write `{slug}.md`, commit, push. Both upsert. |
| `DeleteWikiPage` | ✓ | ✓ | Forgejo: `DELETE /wiki/page/{slug}`. GitHub: refresh clone, `os.Remove`, commit, push. Missing slug → `exitcode.NotFound` on both. |

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

### Wiki backing store

GitHub does not expose a REST surface for wikis — the wiki is just an
independent git repository at `{owner}/{repo}.wiki.git`. Forgejo, by
contrast, has a full REST API (`/repos/{o}/{r}/wiki/pages`,
`/wiki/page/{slug}`, etc.) and never needs a local clone.

To paper over that, the GitHub provider keeps a per-repo working clone
under the user's cache directory:

    $XDG_CACHE_HOME/gaia/wikis/{owner}/{repo}/

(falling back to `~/.cache/gaia/wikis/...` when `XDG_CACHE_HOME` is
unset, per the XDG basedir spec). The cache directory tree is created
with mode `0700`.

**Read paths** (`ListWikiPages`, `GetWikiPage`, `SearchWikiPages`):
on first call the provider shallow-clones the wiki repo
(`git clone --depth 1 --branch master`); on subsequent calls it
serves from disk if the clone's `FETCH_HEAD` mtime is younger than
**5 minutes**, or `git fetch --depth 1 origin master && git reset
--hard origin/master` otherwise. Hard-reset (rather than
`pull --ff-only`) is deliberate: GitHub's wiki history can be
rewritten by the web UI on conflict, and a wiki-cache only cares
about the latest state of each page, not the historical chain.

**Write paths** (`EditWikiPage`, `DeleteWikiPage`): always force-
refresh the clone first so the write lands on top of the latest
upstream state, not a 5-min-stale cache. Then modify the working
tree, `git add -A`, commit with a stable `gaia <gaia@gaia.local>`
identity, and `git push origin master`. Push failure is a hard
error — the provider does NOT auto-rollback the local edit; the
operator's cache is in a divergent state and the next refresh will
surface that loudly.

**Auth**: the configured GitHub PAT is embedded in the clone/push URL
as `https://x-access-token:$TOKEN@github.com/{owner}/{repo}.wiki.git`.
Empty token works for read of public wikis but fails on push. The PAT
is never echoed in error messages — `scrubToken` redacts any captured
git output before it surfaces.

**Branch name**: GitHub wikis ship on `master`, regardless of the
parent repo's default branch. (The wiki feature predates GitHub's
main→default rename and stays on master to avoid breaking existing
clone URLs.) The provider hard-codes `master` in clone, fetch, and
push.

**Slug convention**: `types.WikiPage.Path` is the filename without the
`.md` (or `.markdown`) extension. A page titled "Setup Guide" lives
at `Setup-Guide.md` on disk and `/wiki/Setup-Guide` on the web; the
provider expects callers to pass `Setup-Guide` as the slug, matching
both Forgejo's and the web UI's URL form.

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
5. **GitHub wiki latency**: the first wiki call against a fresh repo
   pays a `git clone` cost (a few hundred ms for typical wikis);
   subsequent reads inside the 5-min TTL are local-disk fast. Forgejo
   wiki calls are always REST hops. Plan agent flows accordingly when
   they're sensitive to first-call cost.

These are documented above and will eventually be addressed in a
Phase 2.5 polish pass.

## How the matrix is verified

Per-method conformance is locked in by two test tiers (see
`docs/testing.md`):

1. **Hand-rolled httptest** in `core/{forgejo,github}/*_test.go`
   pins request shape + trim contract + error mapping for every
   row.
2. **Recorded api.github.com fixtures** in
   `core/github/testdata/fixtures/` exercise the trim pipeline
   against real wire-shapes from the public cli/cli repo (re-record
   via `scripts/record-gh-fixtures.sh`).

If you're updating a row from `△` to `✓`, both tiers should cover
the new behavior before the row flips.

## How this doc is maintained

When a new method joins the Provider interface, add a row here. When
a forge gets a known-different behavior (different endpoint, different
field name on the wire, gap in capability), update the Notes column.

Treat the matrix as the contract that downstream consumers read. If
something's not documented here as "△" or "×", it's expected to
behave identically across providers.
