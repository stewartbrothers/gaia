# Using gaia over MCP

`gaia-mcp` is a Model Context Protocol server that exposes every
gaia operation as an MCP tool. AI agents that speak MCP can talk
to a forge through it without shelling out to `gaia` (saves
process spawn cost) and with native MCP error reporting.

Two transports:

- **stdio** (default): for subprocess hosts (Claude Desktop,
  Cursor, custom in-process agents). Single-tenant, uses the
  current user's layered config + credentials.
- **HTTP** (`--http :addr`): streamable-HTTP transport per the
  2025-03-26 MCP spec, for long-running daemons that remote
  agents pin one URL at. Same tool surface; different transport.

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

Tools added since this section was written: `gaia_release_list`,
`gaia_release_view`, `gaia_release_create`, `gaia_release_edit`,
`gaia_release_delete` — same envelope contract.

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

## HTTP transport

`gaia-mcp --http :8080` runs the streamable-HTTP transport. One
URL serves both direct JSON-RPC responses and SSE streams; an
MCP-aware client picks per-request. The protocol-level handshake
is identical to stdio (`initialize` → `notifications/initialized`
→ `tools/list` / `tools/call`), so any client that works against
stdio works against HTTP with a transport-config switch.

```bash
gaia-mcp --http :8080
gaia-mcp --http :8080 --base-path /v1   # custom URL prefix
gaia-mcp --http :8080 \
  --read-header-timeout 10s \
  --idle-timeout 120s \
  --shutdown-timeout 10s
```

### Smoke test from the shell

```bash
# initialize
SID=$(curl -sS -i -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
       "protocolVersion":"2025-03-26","capabilities":{},
       "clientInfo":{"name":"smoke","version":"0"}}}' \
  | grep -i '^mcp-session-id:' | awk '{print $2}' | tr -d '\r')

# notifications/initialized (required after initialize)
curl -sS -X POST http://localhost:8080/mcp \
  -H "Mcp-Session-Id: $SID" -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'

# call gaia_version
curl -sS -X POST http://localhost:8080/mcp \
  -H "Mcp-Session-Id: $SID" -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{
       "name":"gaia_version","arguments":{}}}'
```

### Timeouts

Defaults are conservative for a small forge proxy. Tune via flags
or env on the deploy unit:

| flag | default | purpose |
|---|---|---|
| `--read-header-timeout` | 10s | slow-loris guard |
| `--idle-timeout` | 120s | keep-alive max idle window |
| `--shutdown-timeout` | 10s | drain on SIGTERM/SIGINT |

The `--shutdown-timeout` matches the orchestrator convention —
Coolify, Kubernetes, ECS all send SIGTERM first then SIGKILL after
a grace period (typically 30s). 10s covers in-flight tool calls
without dragging a deploy.

### Logging

Structured JSON to stderr (slog). One line on listen + one on
shutdown. Production deployments should aggregate stderr — these
are the events a SIEM / Grafana Loki / CloudWatch Logs pipeline
needs to track listener lifecycle.

```json
{"time":"...","level":"INFO","msg":"listening","addr":":8080","path":"/mcp",
 "read_header_timeout":"10s","idle_timeout":"2m0s"}
{"time":"...","level":"INFO","msg":"shutdown","signal":"terminated",
 "drain_timeout":"10s"}
```

### What's deferred

This commit lands the unauthenticated single-tenant transport.
The follow-ups (each with its own issue):

- **#40** — per-request bearer-token auth + multi-tenant config
  (one `gaia-mcp` serves multiple users, each with their own
  forge credentials).
- **#41** — `/healthz` endpoint suitable for orchestrator
  health checks + container-deployment doc with Dockerfile,
  compose example, reverse-proxy guidance.

Until #40 lands, the HTTP transport should only be exposed inside
a trusted network (same docker network as Forgejo, or behind a
reverse proxy that handles auth itself).
