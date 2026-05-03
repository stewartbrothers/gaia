# SQLite cache baseline

Measured via `make cache-bench` (an offline harness with 50 ms
simulated forge latency, 100×issue-view loop). Pinned by PR #152.

| Mode      | Total time | Upstream calls | Notes                  |
|-----------|------------|----------------|------------------------|
| Cached    | **6.5 ms** | **0**          | Same payload returned for every read |
| Uncached  | 5.30 s     | 100            | `--no-cache` flag      |

Speedup ~820× on this offline harness. Real forges (with network
latency, ETag-based 304 responses, and TTL refresh) typically land
50–200×.

The headline behaviour:

- First read: forge round-trip (200ms typical), trim, store.
- Second read within TTL: zero forge call, ~5ms. Cache hit.
- Second read after TTL but unchanged: conditional GET with
  `If-None-Match`, forge returns 304, ~0 bandwidth.
- Read after change: 200, replace cached row.

Re-run with `make cache-bench` to refresh the table.

For real-forge measurement, the harness needs a `cache-bench-live`
target that points at a stable PR/issue and exercises the full
ETag conditional-GET path. Tracked under #43 (search) since the
search baseline will exercise the cache layer end-to-end.
