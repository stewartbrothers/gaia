# Output format

`gaia` and `gaia-mcp` both emit results inside a stable, versioned
envelope. Agents and humans should code against this shape — the
underlying provider responses are explicitly trimmed and re-shaped to
fit it.

## The envelope

Every CLI subcommand prints (and every MCP tool returns) a single JSON
object of this shape:

```jsonc
{
  "schema_version": "1.0",   // bumps on breaking changes only
  "data":            ...,    // the operation result
  "_truncated":      false,  // omitted unless true
  "_next_cursor":    "...",  // omitted unless _truncated
  "_meta":           {...}   // omitted unless populated
}
```

- `schema_version` — bumped only on breaking wire changes. Additive
  changes (a new optional field on a type) keep the version stable;
  removing or renaming a field bumps it.
- `data` — the value the operation produced. List operations return an
  array; single-item operations return one object; scalar operations
  (e.g., `whoami`) return a string or number.
- `_truncated` / `_next_cursor` — pagination state. `_truncated: true`
  means the upstream had more results than were returned. Pass
  `_next_cursor` back in the next call to continue.
- `_meta` — operational side-channel data: rate-limit remaining, cache
  hit/miss, source provider, etc. Read it for diagnostics; do not branch
  business logic on its contents.

The underscore prefix on the meta fields is deliberate: it visually
separates them from `data` payload keys, so an agent that prints the
top-level keys of a response can immediately see what's envelope and
what's content.

## Pagination

| Variable           | Default | Cap   |
|--------------------|---------|-------|
| `--limit`          | 30      | 200   |

CLI subcommands and MCP tools accept `--limit N` (or the `limit`
parameter on MCP). Unspecified, the default of 30 keeps responses
context-window-friendly. Past the cap the request is silently clamped;
`_truncated: true` flags that more results exist upstream.

## Field projection

Every list and view command accepts `--fields a,b,c.d` to filter the
`data` subtree to the listed paths. The envelope's own meta fields
(`schema_version`, `_truncated`, `_next_cursor`, `_meta`) are never
filtered — they are always present.

### Syntax

- Comma-separated list of paths.
- Dotted notation descends into objects: `author.login`.
- Dotted notation also descends into arrays: `labels.name` keeps the
  `name` field of every element of `labels`.
- Whitespace around paths is trimmed.
- Unknown keys are silently dropped.
- Paths that descend past a scalar are silently truncated to the scalar
  (e.g. `--fields a.b` against `{a: 5}` keeps `a: 5`).

### Examples

```bash
# Just the number, title, and state of every PR
$ gaia pr list --fields number,title,state

# A PR view with only labels.name and the head SHA
$ gaia pr view 42 --fields labels.name,head.sha

# Nested: time-ordered comments with author login + body, nothing else
$ gaia pr comments 42 --fields author.login,body,created_at
```

### Why projection lives at the CLI/MCP layer

Provider implementations always return the full trimmed type
(`core/types`); projection happens in the envelope layer at output time.
This keeps the cache layer (Phase 4) and the provider implementations
shape-stable; an agent that asks for many `--fields` projections on the
same underlying object benefits from cache hits even though the wire
output differs per request.

## Schema versions

| Version | Notes                              |
|---------|------------------------------------|
| 1.0     | Initial release (Phase 1).         |

Breaking changes bump the leading digit. Additive changes — new optional
fields, new `_meta` keys — keep the version. Agents should compare the
leading digit, not the full string.

## Exit codes

The exit-code convention is documented separately in
[`exit-codes.md`](exit-codes.md) (lands with #11). In Phase 1 only `0`
(ok) and `1` (generic error) are guaranteed; the full table goes live
once #11 lands.
