# Dogfood comparison

Empirical measurement of `gaia`'s output size vs. `tea` and raw
`curl` calls for the same operations. The point: prove gaia is
producing smaller, agent-shaped responses than the raw alternatives,
not just claim it.

Run the harness yourself:

```bash
make build
GIT_FORGE_GITEA_TOKEN=... ./scripts/dogfood-compare.sh
```

Token estimates use the conventional `bytes / 4` heuristic so you
don't need a tokenizer to reproduce. Lower is better — those bytes
land in the agent's context budget.

## Baseline (2026-05-02, against your-forge.example.com, repo
Gerwood/gaia, PR #75 with no comments)

| Command                                            | Bytes   | ≈Tokens | vs. raw curl |
|----------------------------------------------------|---------|---------|--------------|
| `gaia whoami`                                      | 138     | 34      | **0.21×**    |
| `curl /user`                                       | 647     | 161     | 1×           |
| | | | |
| `gaia issue list` (default, 30 issues)             | 22 639  | 5 659   | **0.35×**    |
| `gaia --fields number,title,state issue list`      | 3 734   | 933     | **0.06×**    |
| `tea issues list --output simple`                  | 3 452   | 863     | 0.05×        |
| `curl /issues?type=issues&state=open&limit=30`     | 65 298  | 16 324  | 1×           |
| | | | |
| `gaia issue view 1`                                | 3 625   | 906     | **0.73×**    |
| `tea issues 1`                                     | 7 772   | 1 943   | 1.57×        |
| `curl /issues/1`                                   | 4 952   | 1 238   | 1×           |
| | | | |
| `gaia pr list` (default, all 30)                   | 44 529  | 11 132  | **0.29×**    |
| `gaia --fields number,title,state,head.ref,base.ref pr list` | 4 491 | 1 122 | **0.03×** |
| `tea pulls list --output simple`                   | 1 954   | 488     | 0.01×        |
| `curl /pulls?state=all&limit=30`                   | 153 942 | 38 485  | 1×           |
| | | | |
| `gaia pr view 75` (no CI)                          | 4 255   | 1 063   | **0.39×**    |
| `gaia pr view 75 --with-ci`                        | 4 387   | 1 096   | **0.40×**    |
| `tea pulls 75`                                     | 7 334   | 1 833   | 0.67×        |
| `curl /pulls/75`                                   | 10 983  | 2 745   | 1×           |
| | | | |
| `gaia pr diff 75` (full structured)                | 111 947 | 27 986  | 1.61×        |
| `gaia --fields path,status pr diff 75`             | 1 620   | 405     | **0.02×**    |
| `curl /pulls/75.diff` (raw text)                   | 69 360  | 17 340  | 1×           |
| | | | |
| `gaia pr comments 75` (empty thread)               | 45      | 11      | **0.38×**    |
| 3× curl (issue/reviews/comments)                   | 118     | 29      | 1×           |
| | | | |
| `gaia search MVP` (1 result)                       | 204     | 51      | **0.04×**    |
| `gaia --fields kind,number,title,repo search MVP`  | 204     | 51      | **0.04×**    |
| `curl /issues?q=MVP&limit=30`                      | 4 954   | 1 238   | 1×           |
| | | | |
| `gaia release list` (5, vs api.github.com cli/cli) | 21 616  | 5 404   | **0.08×**    |
| `gaia --fields tag_name,name,prerelease release list` | 606  | 151     | **0.002×**   |
| `curl /releases?per_page=5` (cli/cli)              | 258 218 | 64 554  | 1×           |
| | | | |
| `gaia release view v2.79.0` (cli/cli)              | 2 958   | 739     | **0.06×**    |
| `curl /releases/tags/v2.79.0` (cli/cli)            | 48 555  | 12 138  | 1×           |
| | | | |
| `gaia packages list --owner X` (#107, est., 1 pkg) | ~140    | ~35     | **~0.10×**   |
| `gaia --fields type,name,version packages list`    | ~80     | ~20     | **~0.06×**   |
| `curl /packages/X?limit=1` (Forgejo)               | ~1 350  | ~338    | 1×           |
| | | | |
| `gaia packages view <type>/<name>/<v>` (#107, est) | ~150    | ~38     | **~0.11×**   |
| `curl /packages/X/<type>/<name>/<v>` (Forgejo)     | ~1 350  | ~338    | 1×           |
| | | | |
| `gaia packages delete <spec> --confirm` (#107)     | ~50     | ~13     | n/a          |
| `curl -X DELETE /packages/X/<t>/<n>/<v>`           | 0       | 0       | (204)        |
| | | | |
| `gaia packages upload generic n v ./a.tgz` (#122)  | ~60     | ~15     | n/a          |
| `curl -T a.tgz /packages/X/generic/n/v/a.tgz`      | 0       | 0       | (201)        |
| | | | |
| `gaia wiki list` (10-page wiki, est.)              | ~600    | ~150    | **0.20×**    |
| `curl /wiki/pages?limit=10` (10-page wiki, est.)   | ~3 000  | ~750    | 1×           |
| | | | |
| `gaia wiki view Home` (1KB markdown body, est.)    | ~1 200  | ~300    | **0.55×**    |
| `curl /wiki/page/Home` (1KB body, base64 wrapped)  | ~2 200  | ~550    | 1×           |
| | | | |
| `gaia wiki search needle` (10 pages, 2 hits)       | ~500    | ~125    | **0.04×**    |
| 1 list + 10 raw GETs (agent's WebFetch loop)       | ~13 000 | ~3 250  | 1×           |
| | | | |
| `gaia webhook list` (3 hooks, est.)                | ~600    | ~150    | **~0.30×**   |
| `gaia --fields id,active,events webhook list`      | ~180    | ~45     | **~0.09×**   |
| `curl /hooks?limit=3` (Forgejo, 3 hooks)           | ~2 000  | ~500    | 1×           |
| | | | |
| `gaia webhook view 7` (1 hook, trimmed)            | ~280    | ~70     | **~0.40×**   |
| `curl /hooks/7` (Forgejo full hook record)         | ~700    | ~175    | 1×           |
| | | | |
| `gaia webhook deliveries 7` (30 deliveries)        | ~3 000  | ~750    | **~0.06×**   |
| `curl /hooks/7/deliveries?limit=30` (with bodies)  | ~50 000 | ~12 500 | 1×           |
| | | | |
| `gaia webhook deliveries 7 --get 101` (full body)  | ~600    | ~150    | **~0.40×**   |
| `curl /hooks/7/deliveries/101`                     | ~1 500  | ~375    | 1×           |
| | | | |
| `gaia webhook redeliver 7 101`                     | ~50     | ~13     | n/a          |
| `curl -X POST /hooks/7/deliveries/101`             | 0       | 0       | (204)        |
| | | | |
| `gaia webhook test 7`                              | ~50     | ~13     | n/a          |
| `curl -X POST /hooks/7/tests`                      | 0       | 0       | (204)        |

> Wiki rows are estimated against a typical 10-page wiki with ~1KB
> markdown bodies. The Forgejo API wraps page bodies in
> `content_base64` and includes URLs, full commit author/committer
> objects, and HTML render hints that gaia drops; the estimated
> ratios mirror what we measure on issue/release reads (5–25× on
> defaults). The `scripts/dogfood-compare.sh` harness will surface
> live numbers once we have a non-empty wiki to point it at.

## Takeaways

### Big wins

- **release list**: 12× smaller than raw curl on a high-asset repo
  like cli/cli. GitHub releases carry the full `assets` array (every
  binary, every download URL, byte counts, content type, uploader,
  state, both URLs per asset) — gaia drops it entirely since agents
  rarely need to enumerate downloadable binaries. With `--fields`
  projection on tag/name/prerelease, the ratio collapses to **400×
  smaller** (250KB → 600 bytes).
- **release view**: 16× smaller than raw curl. Same dynamic — a
  single release record on cli/cli is 48KB raw because of `assets`
  and a verbose `body`; gaia ships 3KB.
- **search**: 24× smaller than raw curl. The trim is dramatic because
  search results in raw form carry full repo objects and PR
  reconciliation fields per result; gaia returns just `{kind, number,
  title, repo}`.
- **whoami**: 5× smaller. Raw `/user` ships avatar URL, email, full
  name, language, last_login, etc.; gaia returns the login.
- **list operations** (`issue list`, `pr list`): 3× smaller by
  default; **15–35× smaller** with `--fields` projection. Fields are
  the killer feature — every list call has 80%+ savings without any
  loss the agent will notice.
- **pr view**: 2.5× smaller than raw curl (and 1.7× smaller than
  `tea`). `--with-ci` adds an extra round-trip server-side but only
  ~3% to the response size.
- **wiki search**: ~25× smaller than the equivalent agent flow of
  `list pages → fetch each → match locally on the agent`. This is the
  headline win for #108: a single MCP `gaia_wiki_search` call collapses
  N WebFetches into one structured response with per-hit snippets.
  The provider layer caps the scan at 100 pages by default
  (`--max-pages`), so very large wikis surface a truncation signal
  rather than spending an unbounded scan budget.
- **wiki view / list**: same shape as releases — Forgejo's full
  `WikiPage` carries `content_base64`, `html_url`, `sub_url`, and a
  full commit author/commiter object; gaia drops everything except
  `title`, `path`, decoded `body`, short SHA, and `updated_at`.
- **webhook deliveries list**: ~16× smaller than raw curl. Raw
  forge responses inline every delivery's full request + response
  bodies — for a `push` event with full commit list this is
  routinely 1.5-5 KB *per delivery*. gaia's list shape carries
  only the summary (id, event, status_code, duration_ms,
  delivered_at, redelivery flag); fetch one detail with
  `gaia webhook deliveries <id> --get N` when you actually need
  the body. This is the headline win for #85: dashboards that
  show 30 recent deliveries to an agent become tractable.

### Packages (#107, estimated)

The packages numbers are estimates rather than measured baselines:
the live forge currently has no published packages, so a real
side-by-side run isn't possible yet. Numbers are derived from the
Forgejo OpenAPI package record shape vs. `types.Package`:

- A raw Forgejo package record carries `id`, `owner` (full user
  object — login, full_name, email, avatar_url, language,
  last_login, html_url, ...), `repository` (similar bloat),
  `creator` (same), `html_url`, `type`, `name`, `version`,
  `created_at`. ~1 350 bytes per record on a typical user.
- gaia's trimmed `types.Package` is `{type, name, version, owner,
  created_at, size?}` — roughly 130 bytes per record. Owner is
  rendered as the login string, never a nested object.

The ratio is consistent with `release` and `issue` paths (~10×
smaller default; ~0.06× with `--fields type,name,version`). When
real packages land we'll re-measure and replace these estimates.

### Honest losses

- **pr diff full structured**: 1.6× *bigger* than raw `.diff` text.
  Each line in a hunk becomes a JSON string with leading marker
  preserved, so the JSON wrapping costs bytes. The win is structure
  (parsed hunks, file metadata) — but if an agent only needs file
  list, `--fields path,status` is **35× smaller than raw**. For
  diffs you almost always want field projection.

### tea comparison

`tea`'s `--output simple` form is plain space-separated text — fewer
bytes than JSON for list operations, but unstructured (agents have
to position-parse). For single-record commands, `tea` is *larger*
than gaia (it adds field labels and formatted blocks).

We compete with `tea` only when an agent could reasonably accept
text. For everything that needs structured output, gaia's JSON is
smaller and easier to consume.

## Write operations (issue/PR/label CRUD)

Write commands (`gaia issue create/edit/comment`, `gaia pr create/
edit/merge`, `gaia label create/edit/delete`) compare differently
than reads — the request body is whatever the user typed, so
"bytes saved on input" is not a meaningful axis. The savings show
up in two other places:

1. **Response-shape trim**, same as reads: gaia parses the upstream
   response into `types.Issue` / `types.PullRequest` / `types.Label`
   and emits them inside the standard envelope. A raw `curl POST`
   response is the full Forgejo record with all the fields gaia
   drops. For the issue-create dogfood (issue #86 created live to
   validate the path), gaia's response was ~600 bytes; the raw
   Forgejo response was ~5KB. Same ~8× ratio as the read paths.

2. **Ergonomics**: the equivalent raw flow is

   ```bash
   curl -sS -H "Authorization: token $TOKEN" \
        -H "Content-Type: application/json" \
        -X POST "$API/repos/$REPO/issues" \
        -d '{"title":"...","body":"...","labels":["bug"]}'
   ```

   vs.

   ```bash
   gaia issue create --title "..." --body "..." --label bug
   ```

   The byte count of the input is similar; the difference is that
   gaia's `--dry-run` prints the wire body for inspection (using the
   option-struct json tags so what you see is what you'd send), and
   `--body -` reads stdin so multiline bodies don't fight your shell
   quoting.

`gaia label create` is the one place where the JSON request body
being structured matters: hex colors must be literal hex without `#`,
and gaia's flag validation (`--color` required, sanity-checked) is
clearer than scripting a curl POST.

## How to update this doc

When a new gaia command lands, add a row here showing gaia bytes vs.
the closest curl/tea equivalent. Re-run the harness against a stable
PR/issue with realistic content; the byte counts will shift as the
forge state changes but the *ratio* should hold.

For write commands, add a paragraph noting the response-shape
savings and call out any UX wins (--dry-run, --body -, validation).

If a gaia command's bytes ratio gets *worse* (relative to raw) at
default settings, that's a regression worth a github-style issue —
the whole product premise is "smaller, agent-shaped output."
