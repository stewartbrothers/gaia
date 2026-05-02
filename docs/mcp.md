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

`gaia-mcp --http <addr>` runs the streamable-HTTP transport. One
URL serves both direct JSON-RPC responses and SSE streams; an
MCP-aware client picks per-request. The protocol-level handshake
is identical to stdio (`initialize` → `notifications/initialized`
→ `tools/list` / `tools/call`), so any client that works against
stdio works against HTTP with a transport-config switch.

### Bind policy

Anyone who reaches the HTTP listener acts as the configured forge
token holder. To prevent accidental public exposure, gaia-mcp
refuses to start in unsafe combinations:

| `--http` binds to | bearer auth (`--token-file`) | `--allow-public-no-auth` | result |
|---|---|---|---|
| `127.0.0.1`, `[::1]`, `localhost` | optional | n/a | start |
| non-loopback (`:8080`, `0.0.0.0:…`, public IP) | configured | n/a | start |
| non-loopback | not configured | `true` | start (proxy in front) |
| non-loopback | not configured | `false` (default) | **refuse** |

`localhost` (and `127.0.0.1`) is the only bind where no-auth is
allowed by default — the network already gates reachability to
the same host. Any other bind requires either bearer auth on
gaia-mcp or an explicit acknowledgment that auth happens upstream
(`--allow-public-no-auth`, intended for reverse-proxy deployments).

```bash
gaia-mcp --http 127.0.0.1:8080                            # local agent
gaia-mcp --http :8080 --token-file /etc/gaia-mcp/tokens   # public, authed
gaia-mcp --http :8080 --allow-public-no-auth              # behind a proxy
```

### Bearer auth

`--token-file <path>` enables `Authorization: Bearer <token>` on
every request. The file format:

```
# comments allowed (full-line, # prefix)
tok_alice                    # bare token; label auto-set "token-N"
tok_bob   alice              # token + space + free-form label
tok_carol bob's-laptop       # multi-word labels are fine
```

The file mode must be `0600` (owner read/write only). gaia-mcp
refuses to start with anything more permissive — same posture
ssh takes for `~/.ssh/id_rsa`. Generate tokens with:

```bash
umask 077
mkdir -p /etc/gaia-mcp
{ echo "$(openssl rand -base64 32) alice@laptop"
  echo "$(openssl rand -base64 32) bob@desktop"; } > /etc/gaia-mcp/tokens
chmod 0600 /etc/gaia-mcp/tokens
```

Constant-time comparison guards against timing-attack token
recovery. Failed auth returns `401 Unauthorized` with
`WWW-Authenticate: Bearer realm="gaia-mcp"` and an opaque body
("Unauthorized") — nothing about *why* the token was rejected.
Detail goes only to the audit log on stderr.

### Audit log

Every authenticated request emits one INFO line:

```json
{"level":"INFO","msg":"auth_success","label":"alice@laptop",
 "remote":"203.0.113.7:54321","path":"/mcp"}
```

Every rejected one emits a WARN with a stable reason enum:

```json
{"level":"WARN","msg":"auth_failure","reason":"unknown_token",
 "remote":"203.0.113.7:54321","path":"/mcp"}
```

Reason values: `no_authorization_header`, `non_bearer_scheme`,
`empty_bearer`, `unknown_token`. The token itself never appears.
The label is what survives token rotation — use it as the stable
identity in dashboards and alerts.

`remote` honors `X-Forwarded-For` when set, so deployments behind
nginx / oauth2-proxy / Cloudflare attribute calls to the real
client. Configure the proxy to **strip client-supplied XFF and set
its own** — multi-element XFF where the leftmost is
client-controlled is spoofable.

### Single-tenant scope

This commit lands one shared bearer namespace: every valid token
authenticates against the *same* upstream forge credentials
(`gaia auth forgejo …` on the host). True multi-tenant — where
token A maps to user A's forge token and token B to user B's — is
a follow-up; tracked alongside the deploy doc in #41.

Practically: today, configure one bearer per agent so the audit
log stays useful (you can tell *which* agent called a tool), but
all agents currently share the operator's forge identity.

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

- **#41** — `/healthz` endpoint suitable for orchestrator health
  checks + container-deployment doc with Dockerfile, compose
  example, reverse-proxy guidance.
- Per-tenant forge credentials (token A → user A's forge token).
  Filed as a #40 follow-up; one deploy of gaia-mcp acting as
  multiple identities is non-trivial config-wise.

The transport is now safe to expose on a public port given a
properly-restricted token file; deploy guidance with full nginx /
docker-compose examples lands with #41.
