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
| `EditIssue` | ✓ | ✓ | `omitempty` on title/body/state/assignees on both. AddLabels/RemoveLabels apply via per-forge label endpoints: Forgejo POSTs `/repos/{o}/{r}/issues/{n}/labels` for adds (after resolving names→IDs via ListLabels) and DELETEs per-ID for removes; GitHub POSTs `/repos/{o}/{r}/issues/{n}/labels` with `{labels:[...]}` for adds and DELETEs per-name for removes. Same callable contract on both. (#327) |
| `CreateIssueComment` | ✓ | ✓ | Same endpoint family; same `{body}` body shape. |
| `EditIssueComment` | ✓ | ✓ | `PATCH /issues/comments/{id}` on both. |
| `DeleteIssueComment` | ✓ | ✓ | `DELETE /issues/comments/{id}`; 204 success. |
| `CreatePullRequest` | ✓ | ✓ | `POST /repos/{o}/{r}/pulls`. Both accept `{title, head, base, body, draft, labels}`. |
| `EditPullRequest` | ✓ | ✓ | `PATCH`. `Draft` is `*bool` so explicitly setting `false` works (flip a draft back to ready). |
| `MergePullRequest` | ✓ | △ | Merge body field names differ. Forgejo: `do`/`MergeTitleField`/`MergeMessageField`/`delete_branch_after_merge`. GitHub: `merge_method`/`commit_title`/`commit_message`. **DeleteBranch is dropped on GitHub** — GitHub's merge endpoint doesn't accept it; the head ref deletion is a separate `/git/refs` DELETE. Tracked as a Phase 2 follow-up. |
| `ListLabels` | ✓ | ✓ | Returns Name + Color + Description on both. `ListLabelsOptions.Name` is a case-insensitive substring filter applied **client-side** on both forges — neither `/labels` endpoint accepts a wire-level filter param (#328). |
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
| `UploadPackage` | △ | × | Forgejo: `PUT /packages/{owner}/generic/{name}/{version}/{file}` with the body streamed as the request body — only `pkgType=generic` is shipped in #122; npm/maven/container/... return a usage error at the boundary because each has a per-protocol publish flow that doesn't share the generic endpoint. GitHub: per-registry publish (npm publish, GHCR Docker v2 push, ...) is genuinely heterogeneous and doesn't fold into one provider method; returns a documented "not implemented" error until per-kind dispatch lands as a follow-up. |
| `ListWebhooks` | ✓ | ✓ | `GET /repos/{o}/{r}/hooks` on both. Forgejo paginates by `?limit=&page=`; GitHub by `?per_page=&page=`. Same trimmed `Webhook` shape (URL/ContentType promoted from each forge's `config.{url,content_type}` nest). |
| `GetWebhook` | ✓ | ✓ | `GET /repos/{o}/{r}/hooks/{id}` on both. 404 → `exitcode.NotFound` on both. Secret is always redacted on read. |
| `CreateWebhook` | ✓ | ✓ | `POST /repos/{o}/{r}/hooks`. Body shape differs: Forgejo uses `{type:"gitea", config, events, active}`; GitHub uses `{name:"web", config, events, active}`. gaia maps both into a single `CreateWebhookOptions`. |
| `EditWebhook` | ✓ | ✓ | `PATCH /repos/{o}/{r}/hooks/{id}`. **Different merge semantics**: GitHub accepts `add_events`/`remove_events` directly so gaia passes them through verbatim; Forgejo only accepts a full `events` list, so gaia pre-fetches and merges client-side. Same callable contract (`AddEvents`/`RemoveEvents` on options); one extra round-trip on Forgejo only. |
| `DeleteWebhook` | ✓ | ✓ | `DELETE /repos/{o}/{r}/hooks/{id}`; 204 success on both. |
| `ListWebhookDeliveries` | ✓ | ✓ | `GET /repos/{o}/{r}/hooks/{id}/deliveries`. Both forges return delivery summaries; bodies are NOT inlined (use `GetWebhookDelivery` for the per-delivery full payload). Forgejo's `duration` field has shipped both as seconds-as-float and ns-as-int across versions; gaia normalizes to integer milliseconds via `durationToMs`. |
| `GetWebhookDelivery` | ✓ | ✓ | `GET /repos/{o}/{r}/hooks/{id}/deliveries/{delivery_id}`. Carries full request + response headers/body. Forgejo flattens into `request_headers`/`request_body`/`response_headers`/`response_body`; GitHub nests under `request.{headers,payload}` and `response.{headers,payload}`. gaia maps both into the unified `WebhookDeliveryDetail`. |
| `RedeliverWebhook` | ✓ | ✓ | **Different paths**: Forgejo uses `POST /hooks/{id}/deliveries/{delivery_id}` (post-to-the-resource); GitHub uses `POST /hooks/{id}/deliveries/{delivery_id}/attempts`. GitHub returns 202 (async); Forgejo returns 204 (sync). Both mapped to nil error on success. |
| `TestWebhook` | ✓ | ✓ | `POST /repos/{o}/{r}/hooks/{id}/tests` on both. Forgejo dispatches a synthetic ping payload; GitHub dispatches a `push` event using the repo's most recent commit. 204 success. |
| `ListIssueDependencies` | ✓ | ✓ | Forgejo: `GET /repos/{o}/{r}/issues/{n}/dependencies`. GitHub: `GET /repos/{o}/{r}/issues/{n}/dependencies/blocked_by` (REST landed in API version 2026-03-10 — #326). Same `[]types.Issue` return shape on both. |
| `ListIssueBlocks` | ✓ | ✓ | Forgejo: `GET /repos/{o}/{r}/issues/{n}/blocks` — the inverse view. GitHub: `GET /repos/{o}/{r}/issues/{n}/dependencies/blocking`. |
| `AddIssueDependency` | ✓ | △ | Forgejo: `POST /repos/{o}/{r}/issues/{n}/dependencies` with body `{"index": M}` for same-repo, or `{"index": M, "owner": "o2", "repo": "r2"}` for cross-repo (#325). GitHub: `POST /repos/{o}/{r}/issues/{n}/dependencies/blocked_by` with body `{"issue_id": M}` where M is the issue's **internal stable id**, not its user-facing number; cross-repo refs target the dep's repo for the number→id resolve, then POST to the host's blocked_by endpoint with the resolved id. The Provider contract takes a `dep provider.IssueDepRef` callers think of in {Number, optional Owner+Repo} terms; the github provider transparently resolves number → id via an extra `GET /issues/{dep}` round-trip before the write. |
| `RemoveIssueDependency` | ✓ | △ | Forgejo: `DELETE /repos/{o}/{r}/issues/{n}/dependencies` with body `{"index": M}` (same omitempty Owner/Repo extension as Add for cross-repo). GitHub: `DELETE /repos/{o}/{r}/issues/{n}/dependencies/blocked_by/{issue_id}` — id in path, no body. Same number → id resolve (cross-repo aware) as Add. 404 → `exitcode.NotFound` on both. |

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

### Webhooks

Both forges expose CRUD + delivery history + redeliver + test under
the same `/repos/{o}/{r}/hooks` URL family, but the wire shapes differ
at three points worth knowing about:

1. **Edit-time event-list merge.** GitHub's edit endpoint accepts
   `add_events`/`remove_events` body fields directly; Forgejo's only
   accepts a full new `events` list. gaia presents one unified
   `EditWebhookOptions` shape (`AddEvents`/`RemoveEvents`); the
   Forgejo provider does a pre-fetch round-trip to compute the
   merged set client-side, the GitHub provider doesn't.

2. **Redeliver path.** Forgejo: `POST /hooks/{id}/deliveries/{deliveryID}`
   (sync, 204). GitHub: `POST /hooks/{id}/deliveries/{deliveryID}/attempts`
   (async, 202).

3. **Delivery payload shape.** Forgejo flattens request/response
   headers + bodies onto the delivery record top-level
   (`request_headers`, `request_body`, etc.). GitHub nests them
   under `request.{headers,payload}` and `response.{headers,payload}`.
   The unified `WebhookDeliveryDetail` shape carries flat
   `RequestHeaders` + `RequestBody` + `ResponseHeaders` + `ResponseBody`;
   the per-forge code unwraps as needed.

**Secrets are write-only.** The trimmed `Webhook` type does NOT
have a `Secret` field; both forges redact secret on read, and the
gaia API surface preserves that — secrets travel only in
`CreateWebhookOptions.Secret` / `EditWebhookOptions.Secret`. CLI
`--dry-run` prints `<redacted>` rather than the actual value so it
never lands in shell history or terminal scrollback.

**Delivery payload sizes.** A single delivery for a busy repo's
`push` event can be 50–200 KB. The `ListWebhookDeliveries` shape
deliberately does NOT inline request/response bodies — agents
inspect summaries first, fetch one detail with `GetWebhookDelivery`
when they need the body. The CLI mirrors this with
`gaia webhook deliveries <id> --get N`.

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
