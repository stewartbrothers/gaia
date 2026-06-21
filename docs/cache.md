# Local read cache

Issue: [#42](https://github.com/stewartbrothers/gaia/issues/42).

`gaia` keeps a small SQLite-backed read cache so repeated forge
reads (issue view, PR view, list calls) skip the upstream
round-trip when the cached payload is fresh. Stale entries trigger a
conditional `If-None-Match` / `If-Modified-Since` GET — a 304 means
"reuse the cached bytes", a 200 means "replace the row". Both paths
shave latency and trim upstream traffic.

The cache is **opt-in via config** but defaults to `enabled: true`.
Per-call bypass: `gaia --no-cache <cmd>`. Full wipe:
`gaia cache nuke`.

## Layout

One SQLite file per `(provider, host)`:

```
~/.cache/gaia/
├── forgejo/
│   ├── your-forge.example.com.db
│   └── codeberg.org.db
├── github/
│   └── api.github.com.db
└── meta/
    └── autodetect.db        # non-forge: git-remote parses (see Typed[T] below)
```

The `meta/` file is not a forge cache. It holds provider-independent
local lookups — currently just parsed git remotes keyed by checkout
path — that have to be resolved *before* a provider (and therefore a
`(provider, host)` file) is known. `gaia cache nuke` clears it along
with the forge files, since nuke walks every `.db` under the root.

Honors `$XDG_CACHE_HOME` (falls back to `$HOME/.cache`). Parent
directories are created with mode `0700`; cache files with mode
`0600`. One file per origin gives:

- **Cache-poisoning isolation** — a compromised forge cannot pollute
  another forge's cache.
- **Trivial nuke** — `rm ~/.cache/gaia/forgejo/host.db` works.
- **Multi-process safety** — SQLite's file locking + `journal_mode=WAL`
  let concurrent gaia processes share the same file without
  corruption. WAL also means readers don't block writers.

### Schema

Two tables. Object reads land in `objects`; list-style queries
(`gaia issue list`, `pr list`, etc.) get a parallel `list_index`.

```sql
CREATE TABLE IF NOT EXISTS objects (
  kind          TEXT NOT NULL,           -- 'issue', 'pr', 'comment', 'wiki', 'release', 'package'
  owner         TEXT NOT NULL,
  repo          TEXT NOT NULL,           -- "" for owner-scoped (packages)
  id            TEXT NOT NULL,           -- '42', 'Home', 'npm/foo/1.0.0'
  etag          TEXT,
  last_modified TEXT,
  fetched_at    INTEGER NOT NULL,
  ttl_seconds   INTEGER NOT NULL,
  payload       BLOB NOT NULL,           -- JSON of the trimmed type
  PRIMARY KEY (kind, owner, repo, id)
);

CREATE TABLE IF NOT EXISTS list_index (
  kind          TEXT NOT NULL,
  owner         TEXT NOT NULL,
  repo          TEXT NOT NULL,
  query_hash    TEXT NOT NULL,           -- sha256 of canonical query JSON
  fetched_at    INTEGER NOT NULL,
  ttl_seconds   INTEGER NOT NULL,
  next_cursor   TEXT,
  payload       BLOB NOT NULL,
  PRIMARY KEY (kind, owner, repo, query_hash)
);
```

Schema migrations are pure `CREATE … IF NOT EXISTS` — they re-run on
every Open, idempotent by design (same pattern noted in the
project's `CLAUDE.md` "Container Deploys" section). The file lives in
`core/cache/schema.go`.

The cached payload is **always the trimmed `core/types` JSON**, never
the raw forge response. Three reasons:

1. **Token economy** — agents already see the trimmed shape; caching
   the trimmed shape preserves the savings.
2. **Trust markers survive** — `Issue.Body` is tagged
   `gaia:"trust=external"` (#146); a cached → retrieved → re-marshalled
   value still emerges with `_trust=external`. Pinned by
   `TestTrustMarkerSurvivesCacheRoundtrip`.
3. **No regression vector** — if the trim layer ever loses a field, a
   cache that stores raw JSON could leak it via replay long after the
   upstream patched it.

## TTL semantics

| Operation | Default TTL | Why |
|---|---|---|
| Single-resource read with ETag (`gaia issue view`, `pr view`) | 5 min | ETag handles correctness; TTL bounds bandwidth. |
| List read (`gaia issue list`, `pr list`) | 30 sec | No reliable ETag on lists; tight staleness window. |
| Cross-resource search | 60 sec | Same as lists. |
| **CI-monitoring read** (`pr view --with-ci`, `pr ci-wait`) | **never cached** | A CI read is keyed off the PR's live head SHA; a cached PR would key the status lookup off a stale SHA after a force-push and report the previous commit's result as current (#367). |

**CI status is never served from cache.** `GetPullRequest(WithCISummary)`
bypasses the cache for the PR read (so the head SHA is live), and the
status/check-runs lookup is always a plain `GET`. `pr ci-wait` therefore
always polls fresh state without needing `--no-cache`, and any chain that
shells out to `ci-wait` inherits that. Commit statuses and Actions runs
(`gaia actions list|view`, `pr ci-wait --ref`) are likewise never cached.

A TTL'd entry is **not deleted on expiry** — it's marked stale and
re-validated. The HTTP path issues a conditional GET:

- 304 → reuse the cached bytes; bump `fetched_at`.
- 200 → trim, replace the row, capture the new ETag/Last-Modified.

This is RFC 7232 cache-revalidation; gaia's contribution is the
trim-on-the-way-in step that keeps the cache shape consistent with
the JSON the user would have seen anyway.

### Configurable

Drop a `cache:` block into `~/.config/gaia/config.yaml`:

```yaml
cache:
  enabled: true              # default true; set to false to disable globally
  ttl_seconds:
    single: 300              # 5 min — single-resource reads
    list: 30                 # 30 s  — list reads
  max_size_mb: 100           # vacuum target (planned, not yet enforced)
```

Per-call override: `gaia --no-cache <cmd>` bypasses the cache for one
invocation.

## Write invalidation

Mutating provider methods evict affected keys via
`core/cache/invalidate.go`. Policy:

- `Edit*` — drop the matching object row + flush every list_index
  row for the repo (state changes can move the resource between
  filtered lists).
- `Create*` — flush every list_index row for the repo (new resource
  could appear in any filter).
- `Delete*` — drop the object + lists.
- `Merge` (PR) — same shape as `Edit*`.

A nil cache passes straight through, so the wiring is a no-op when
caching is disabled. Future mutations whose impact is too broad to
enumerate fall back to `Invalidator.FlushRepo` — soft flush of every
list_index row for the repo.

## For non-HTTP callers: `cache.Typed[T]`

The sections above describe how the **HTTP clients** use the cache:
raw response bytes, ETags, conditional GETs, list invalidation. That
machinery is overkill for code that just wants "give me this value,
computing it on a miss" — autodetect's git-remote parse, a doctor
probe result, a chain step's memoised lookup.

`cache.Typed[T]` is the narrow, type-safe slice for exactly that. It
wraps any `cache.Cache` and speaks in a Go type `T`, hiding the
`Key`/`Entry` plumbing and the JSON marshalling behind one call:

```go
gitRemotes := cache.Typed[*autodetect.Repo]{
    Cache: c,                 // any cache.Cache; nil = no caching, passthrough
    Kind:  "git-remote",      // partitions the key space
    TTL:   24 * time.Hour,
}

// Lookup → miss → fetch → store, all in one call. fetch runs at most
// once per miss; a live hit never calls it.
repo, err := gitRemotes.GetOr(ctx, "", name, absPath, func(ctx context.Context) (*autodetect.Repo, error) {
    return autodetect.FromGitRemote(dir, name)
})
```

Contract:

- **`GetOr(ctx, owner, repo, id, fetch)`** returns the cached value or,
  on a miss/stale/undecodable entry, calls `fetch` exactly once, stores
  the result under the `Typed`'s `TTL`, and returns it. A `fetch` error
  propagates verbatim and **nothing is stored** — a failed upstream call
  never pollutes the cache. The cache layer itself never surfaces an
  error from `GetOr`: lookup, decode, and store failures all degrade to
  "treat as a miss" / "the fetched value is still valid".
- **`Invalidate(ctx, owner, repo, id)`** drops one entry.
- **`InvalidateList(ctx, owner, repo)`** drops cached list rows for the
  `Kind` (companion for callers that also cache lists under it).
- An empty `owner` is mapped to a fixed sentinel so owner-less,
  path-keyed values (like git remotes) satisfy the underlying store's
  non-empty-owner contract without aliasing a real forge owner.
- A **nil `Cache`** makes `GetOr` a plain `fetch` passthrough and the
  invalidators no-ops, so callers wire it unconditionally and let the
  "caching off" case fall through.

The first adopter is `autodetect.FromGitRemoteCached`, which backs the
`meta/autodetect.db` file above. See `core/cache/typed.go` and its
runnable `ExampleTyped`.

## MCP transport: per-tenant safety

The HTTP MCP transport is multi-bearer: one gaia-mcp process can
serve agents holding different forge PATs. The cache is **shared
across bearers for hits, never populated using a non-requester's
bearer for misses**:

1. Lookup uses `(kind, owner, repo, id)` — bearer-independent.
2. On a hit, the cached payload is returned to whoever is calling.
3. On a miss, the upstream call fires with **the requester's
   bearer**, the response is trimmed, and the row is stored under
   the same bearer-independent key.

The cache row's payload is the trimmed type — same data every bearer
with read access would see. The cross-tenant info-leak surface is
**"did this resource exist?"** — acceptable for forge metadata that
is auditable upstream anyway. If your forge has private resources
where existence itself is sensitive, set `cache.enabled: false` in
config.

## Hardening

- Cache file mode `0600`, parent `0700`. Belt-and-braces:
  `Open` re-`chmod`s in case the dir pre-existed with looser perms.
- All payloads are the trimmed `core/types` JSON, never the raw
  forge response. A `#146` regression on the trim layer cannot leak
  via cache replay because the cache only ever stored trimmed
  bytes in the first place.
- Trust markers survive a cache roundtrip (regression test
  `TestTrustMarkerSurvivesCacheRoundtrip`).
- Driver: `modernc.org/sqlite` v1.34.5 — pure Go, no cgo. Keeps the
  goreleaser cross-compile pipeline (#48) clean.
- `journal_mode=WAL` so concurrent gaia processes don't block each
  other on reads. `busy_timeout=5000` gives writers 5s to win the
  lock instead of erroring.

## Troubleshooting

### "I edited an issue out-of-band and gaia is showing stale data"

Per-call bypass:

```bash
gaia --no-cache issue view 42
```

That re-fetches and updates the cache. Subsequent calls see the
fresh entry.

### "I want to wipe the cache after a forge migration (ETags reset)"

```bash
gaia cache nuke                              # everything
gaia cache nuke --provider forgejo           # one provider
gaia cache nuke --provider forgejo --host your-forge.example.com       # one host
```

The command also removes `*-wal` and `*-shm` SQLite sidecar files.

### "How big is my cache?"

```bash
du -sh ~/.cache/gaia
ls -lh ~/.cache/gaia/forgejo/
```

The `max_size_mb` knob in config is reserved for a vacuum-on-write
that lands with #43; until then it's an informational ceiling.

## Out of scope (in this PR)

The following land in follow-up PRs against the Phase 4 epic:

- Cross-resource search (#43) — uses the cache layer landed here.
- NDJSON streaming (#46) — same.
- Background eviction daemon — vacuum is on-write only for now.
- Remote-cache mode (Redis-backed shared cache for HTTP MCP fleets).
