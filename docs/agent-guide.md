# Agent guide

Short, dense pointers for AI agents (and humans) using gaia
efficiently. The full reference is in the rest of `docs/`; this page
is the cliff-notes.

## TL;DR

| If you want to... | Use |
|---|---|
| Confirm auth works | `gaia whoami` |
| List issues | `gaia issue list --state open --fields number,title,state` |
| View an issue | `gaia issue view 42 --with-comments 5` |
| List PRs | `gaia pr list --state all --fields number,title,state` |
| View a PR + CI | `gaia pr view 42 --with-ci` |
| Get a PR's diff | `gaia pr diff 42 --fields path,status` (use `--paths` to narrow) |
| Read all PR feedback | `gaia pr comments 42` (merges issue + review + inline) |
| Search across | `gaia search "memory leak" --kind issue --fields number,title,repo` |
| Open an issue | `gaia issue create --title "..." --body "..."` |
| Comment | `gaia issue comment 42 --body "..."` |
| Open a PR | `gaia pr create --title "..." --head feature/x --base main --body "..."` |
| Submit a review | `gaia pr review 42 --state approve --body "..."` |
| Merge a PR | `gaia pr merge 42 --method squash` |

## Save tokens with `--fields`

Field projection is the single biggest win. Default `gaia issue list`
returns the full Issue type per row; with `--fields number,title,state`
you get just three keys. Combine with `--limit` to cap the page.

```bash
$ gaia issue list                          # ~22KB for 30 issues
$ gaia issue list --fields number,title    #  ~3KB for 30 issues
```

Common projections:

- `number,title,state` — minimal list view
- `number,title,labels.name` — list + labels
- `path,status` for `pr diff` — file-list-only (35× smaller than full)
- `kind,number,title,repo` for `search` — minimal hit info

## State-checks vs full views

`gaia pr view 42` returns the full PR. If you only need state +
mergeable + ci status:

```bash
gaia pr view 42 --with-ci --fields number,state,mergeable,ci_summary.state
```

`--with-ci` triggers an extra `/status` round-trip; skip it if the
mergeable bit is enough.

## Error handling

Exit codes branch the way agents need:

- `0` ok
- `2` Usage — your request shape is wrong
- `3` NotFound — the resource doesn't exist
- `4` Auth — token missing or rejected; rerun `gaia auth ...`
- `5` RateLimit — back off, then retry
- `6` Network — short backoff, then retry

```bash
gaia pr view 42
case $? in
  0) ;;
  3) echo "PR doesn't exist" ;;
  5) sleep 60 ;;
  6) sleep 5 ;;
  *) handle ;;
esac
```

## Reading bodies from stdin

Long markdown bodies don't play well with shell quoting. Use
`--body -`:

```bash
cat <<EOF | gaia issue create --title "boom" --body -
## Stack trace
...
EOF
```

Same convention for `gaia issue comment`, `gaia pr create`,
`gaia pr review`, `gaia pr edit`.

## Dry-run before write

Every write subcommand accepts `--dry-run`. The output is the
literal HTTP method + path + JSON body — useful when scripting flows
and uncertain whether the option struct serializes the way you
expect.

```bash
$ gaia pr create --title "..." --head feature/x --base main --dry-run
POST /repos/Gerwood/gaia/pulls
{
  "title": "...",
  "head": "feature/x",
  "base": "main"
}
```

## MCP integration

If you're an MCP-aware agent, prefer `gaia-mcp` (stdio) over shelling
out to `gaia`. Same envelope shape, no process spawn cost per call.
See [`mcp.md`](mcp.md).

## Don't use `curl` if `gaia` covers it

The product premise is "agent-shaped, token-trimmed responses." Raw
curl returns full Forgejo records (~5–20× more bytes for the same
information). The dogfood discipline:

- For any read or write `gaia` supports → `gaia`.
- For unsupported gaps (releases, webhooks for now) → curl with the
  documented Forgejo API endpoints (CLAUDE.md "Raw API for gaps"
  section lists them).

The list of currently-supported operations is in
[`README.md`](../README.md). When in doubt, run `gaia --help` and
look for the resource you need.
