# Dogfood note: HTMLURL on Issue + PullRequest (#305)

## What changed

`types.Issue` and `types.PullRequest` now carry `HTMLURL string`,
threaded through from the upstream `html_url` field on both the
Forgejo and GitHub providers. Mirrors the same field on
`types.WorkflowRun` (lands with #266/#267 in actions.go).

Agents redirecting humans to the forge no longer have to reconstruct
the URL from `--api-url` + owner/repo/number — that reconstruction is
brittle (assumes the Forgejo / GitHub UI URL convention holds) and
costs an extra `whoami` round-trip on top.

## Byte cost

Measured 2026-05-21 against repo `Gerwood/gaia` on
`git.stewartbrothers.com.au` (Forgejo). Token estimates use
`bytes / 4`.

| Command                                            | Pre-#305 | Post-#305 | Delta |
|----------------------------------------------------|----------|-----------|-------|
| `gaia issue view 299` (single record)              | ~2 360   | 2 432     | +72 B (+3.1%) |
| `gaia issue list --state all --limit 30`           | ~16 200  | 17 577    | +1.4 KB (+8.5%) |
| `gaia --fields number,title,html_url issue view 299` | n/a    | 247       | URL-only projection |

The single-record cost is the URL length itself
(`https://git.stewartbrothers.com.au/Gerwood/gaia/issues/299` ≈ 58
chars) plus the JSON envelope overhead (`"html_url":""` + comma ≈ 14
bytes). List-call delta scales linearly with item count.

The win: callers who don't need the URL drop it cleanly with
`--fields`. Callers who do need it skip:

```bash
# Pre-#305 workaround (brittle, 1 extra round-trip):
HOST=$(gaia whoami --fields host --format json | jq -r .data.host)
URL="https://${HOST}/Gerwood/gaia/issues/299"

# Post-#305:
gaia issue view 299 --fields html_url --format json | jq -r .data.html_url
```

## CLI shape

No new flags. `--fields html_url` projection now resolves to a real
field instead of silently filtering to empty.

```bash
gaia issue view 42 --fields number,title,html_url
gaia issue list --fields number,title,html_url --state open
gaia pr view 75 --fields number,title,html_url,state
gaia pr list --fields number,title,html_url --state open
```

## Provider coverage

- ✅ Forgejo (`core/forgejo/issues.go`, `core/forgejo/pull_requests.go`)
- ✅ GitHub  (`core/github/issues.go`,  `core/github/pull_requests.go`)

Tests pinning the field round-trip live alongside each provider's
existing list+view tests (`TestListIssuesPreservesHTMLURL`,
`TestGetIssuePreservesHTMLURL`, and PR equivalents — same shape on
both providers).

## Out of scope

- Adding URL fields to other resources (releases, labels, milestones,
  webhooks). Each is a separate cost/benefit call when the use case
  comes up.
- A `gaia issue url` / `gaia pr url` shortcut command. Today
  `--fields html_url` covers the use case; a dedicated command would
  only save typing.
