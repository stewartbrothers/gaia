# Using gaia over MCP

`gaia-mcp` is a Model Context Protocol stdio server that exposes
every gaia operation as an MCP tool. AI agents that speak MCP can
talk to a forge through it without shelling out to `gaia` (saves
process spawn cost) and with native MCP error reporting.

## Configuring an MCP-aware client

For Claude Desktop, edit `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "gaia": {
      "command": "/path/to/gaia-mcp",
      "env": {
        "FORGEJO_TOKEN": "...",
        "FORGEJO_API_URL": "https://your-forge.example.com/api/v1",
        "GAIA_PROVIDER": "forgejo"
      }
    }
  }
}
```

If you've already run `gaia auth forgejo <url>` on the same machine,
the env block can be omitted entirely — `gaia-mcp` uses the same
layered credentials store as the CLI.

## Tool surface

Read tools:

- `gaia_version`, `gaia_whoami`
- `gaia_issue_list`, `gaia_issue_view`
- `gaia_pr_list`, `gaia_pr_view`, `gaia_pr_diff`, `gaia_pr_comments`
- `gaia_label_list`
- `gaia_search`

Write tools:

- `gaia_issue_create`, `gaia_issue_edit`, `gaia_issue_comment`
- `gaia_pr_create`, `gaia_pr_edit`, `gaia_pr_merge`, `gaia_pr_review`
- `gaia_label_create`, `gaia_label_edit`, `gaia_label_delete`

Every tool that returns data wraps it in the standard envelope
(`schema_version`, `data`, `_truncated?`, `_next_cursor?`) — the same
shape `gaia <verb> --format json` produces. A tool's response is
just the JSON-encoded envelope inside an MCP `text` content block.

## Argument conventions

- `repo` is required and takes `owner/name`.
- `number` is the issue/PR number (positive integer).
- Arrays are JSON arrays (e.g. `["bug", "p1"]`); strings are strings;
  numbers are JSON numbers.
- Optional fields can be omitted entirely; they don't need explicit
  null values.

## Inline review comments

`gaia_pr_review` accepts an array of inline comments:

```json
{
  "name": "gaia_pr_review",
  "arguments": {
    "repo": "Gerwood/gaia",
    "number": 75,
    "state": "request-changes",
    "body": "see inline",
    "comments": [
      {"path": "core/x.go", "line": 42, "body": "rename this"},
      {"path": "core/y.go", "line": 18, "body": "tighten loop"}
    ]
  }
}
```

`line` is mapped to `new_position` on the upstream wire (line in the
post-change file). Old-side commenting isn't yet exposed.

## Errors

Tool errors come back as MCP tool-result errors with the underlying
gaia error message. The exit-code wrapping is preserved: a 401 still
threads `exitcode.Auth` through `errors.As` so a wrapper can react
the same way the CLI does.

Transport errors (server died, etc.) come back as MCP RPC errors via
the protocol's standard channel.

## Pagination

List tools accept `cursor` and return `_next_cursor` in the envelope
when truncated. Pass the cursor back unchanged on the next call.

```json
// First call
{"name": "gaia_issue_list", "arguments": {"repo": "o/r", "limit": 30}}
// → {"data": [...30 issues...], "_truncated": true, "_next_cursor": "2"}

// Continue
{"name": "gaia_issue_list", "arguments": {"repo": "o/r", "limit": 30, "cursor": "2"}}
```

## Phase 3 transport

`gaia-mcp` currently speaks stdio only. Phase 3 (#39) will add an
HTTP/SSE transport so a single `gaia-mcp` process can serve multiple
remote agents. Configuration shape will mirror the stdio path —
existing tool definitions don't change.
