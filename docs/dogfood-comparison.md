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
