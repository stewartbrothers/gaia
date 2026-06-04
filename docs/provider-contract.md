# Provider contract

`core/provider.Provider` is gaia's central interface — the seam between
the frontends (`cmd/gaia`, `cmd/gaia-mcp`) and the per-forge
implementations (`core/forgejo`, `core/github`). This document pins the
cross-cutting rules every implementation must honour and every caller
may rely on. Per-method docstrings on `provider.go` cover the
operation-specific shape; this file covers everything that would
otherwise have to be re-derived from reading implementation code.

The contract is mandatory: a Provider implementation that violates any
section below is buggy, even if its tests pass. A caller that depends
on behaviour outside this contract is buggy, even if it works today.

## 1. Error contract

Every method returns either `nil` or an `*exitcode.Error` (see
`core/exitcode`). Implementations never return raw `error` values from
upstream packages — `exitcode.Wrap` and `exitcode.FromHTTP` are the
adapters.

HTTP status → exit code mapping is centralised in `exitcode.FromHTTP`
and is part of the contract:

| Upstream | Exit code | Caller meaning |
|---|---|---|
| 401, 403 | `Auth` | token missing, expired, or unauthorised for this resource |
| 404 | `NotFound` | resource absent |
| 409, 422 | `Conflict` | precondition failed (already-merged PR, label already exists) |
| 5xx (after retry) | `Network` | upstream unhealthy |
| transport error | `Network` | DNS, TLS, timeout, connection refused |
| anything else | `Generic` | unmapped — file a bug |

Errors carry no token material. The `scrubError` helper in each
forge's `client.go` is responsible for stripping `Authorization`
headers and bearer tokens before the error bubbles. Callers may log
the returned error directly.

Callers branch on exit code via `errors.As` plus `exitcode.Code()`,
never on string matching.

## 2. Retry and idempotency

Implementations MAY retry. The retry contract:

- **GET** retries exactly once on 5xx, after `RetryWait` backoff
  (default 500ms). 4xx is never retried.
- **POST, PATCH, PUT, DELETE** are never retried — write idempotency
  is the caller's responsibility on the rare paths it matters
  (release-asset upload uses the "skip if name exists" guard;
  `EditWebhook` pre-fetches before merging events).
- A retry is invisible to the caller: it sees one error or one
  success, never two.

Methods that are naturally idempotent on the forge side are documented
as such on their docstring (e.g. `DeleteLabel` is safe to call twice;
the second call returns `NotFound`, which callers may ignore).

## 3. Pagination

List methods return `([]T, *Page, error)`. The `Page` value is the
contract for "is there more":

- `nil` Page → no pagination state (single-shot endpoints like
  `ListLabels`, or trivially-small results).
- `Truncated = true` → the caller did not see every result. They MUST
  use `NextCursor` to fetch more if completeness matters.
- `NextCursor != ""` → opaque cursor; pass back via the same method's
  `Cursor` option field.
- Empty cursor with `Truncated = false` → fully drained.

Cursors are opaque: callers don't parse them, don't reorder them, and
don't construct them. Implementations may change cursor format without
notice; the wire format is implementation-private.

Limit clamping: requests above `MaxLimit` (200) are silently clamped.
The clamp is visible via `Truncated = true`. There is no error for
"limit too high".

Ordering of list results is **newest-first** unless the method's
docstring says otherwise. Implementations honour the forge's natural
ordering; cross-forge consistency for, e.g., `ListIssues` is part of
the parity test suite.

## 4. Trust tagging

Every field on `core/types` that carries forge-originated user input
is tagged `gaia:"trust=external"`. Examples: `Issue.Body`,
`Comment.Body`, `PullRequest.Title`, `WikiPage.Body`, `Release.Body`.

Provider implementations MUST NOT strip this tag, override it, or
copy field contents into untagged fields. The envelope's
`MarshalJSON` walks the struct tag at the wire boundary; bypassing
the tag is a security regression (#146).

Operator-supplied fields (login names, labels, milestone titles
chosen by the operator) are NOT tagged. The line is: did the bytes
arrive from a third party who could write whatever they wanted? If
yes, tag it.

## 5. Concurrency

Provider instances returned by `forgejo.NewProvider` and
`github.NewProvider` are safe for concurrent use by multiple
goroutines. The underlying `*http.Client` and `cache.Cache` are
concurrency-safe; the Provider holds no per-call mutable state.

Callers do not need to serialise Provider use; chain steps,
`pr ci-wait` polling, and any future fan-out feature share one
Provider value.

## 6. Context

Every method takes a `context.Context`. Implementations:

- Honour cancellation immediately on every upstream call.
- Propagate cancellation through the cache layer (cache lookups are
  cheap but still take a context for symmetry).
- Return `context.Canceled` or `context.DeadlineExceeded` unchanged
  (these are exempt from the `*exitcode.Error` wrapping rule, so
  callers can branch on them with `errors.Is`).

A nil context is a programming error.

## 7. Cache transparency

Implementations MAY cache reads. The cache:

- Is invisible to callers. A cache hit and a fresh upstream call
  return identical values; the caller cannot tell which it got.
- Honours `--no-cache` via the `Options.Cache = nil` construction
  path. With a nil cache, every read goes upstream.
- Stores trimmed payloads only — never raw forge JSON. This keeps
  the trust-tag contract intact across cache round-trips.
- Returns stale data through the conditional-GET protocol (#42):
  stale entries trigger an `If-None-Match`/`If-Modified-Since`
  request; a 304 confirms the cached bytes.

Cache failures are not surfaced. A cache lookup error, a corrupt
cached payload, or a store failure all degrade to "fetch upstream";
the caller sees a normal upstream call. Logging cache failures is
the implementation's choice.

## 8. Write results and re-fetch

Write methods that mutate state (`CreateIssue`, `EditIssue`,
`CreatePullRequest`, etc.) return the **trimmed post-write view**.
The returned value reflects the forge's state immediately after the
write, including any defaults the forge applied (timestamps, IDs,
default labels).

Write methods that return `error` only (`MergePullRequest`,
`DeleteLabel`, `RerunWorkflowRun`, etc.) do not include a re-fetch.
Callers that need the post-write state call the corresponding `Get`
or `List` method.

Write methods MUST invalidate any cache entries they affect. This is
the responsibility of each implementation; the cache layer offers
`cache.NewInvalidator(...).AfterUpdate/AfterDelete` helpers.

## 9. Nil and zero values

- A nil Provider is a programming error; callers do not check.
- A nil `*Page` from a list method means "no pagination state" (see
  §3); it does NOT mean "no results" (an empty slice means that).
- A nil pointer returned from a Get method is a contract violation;
  the method MUST return either a non-nil pointer or an error.
- Empty option fields are "no change" on Edit methods. Explicit
  zeroing of a forge field (e.g., clearing a milestone's due date)
  is exposed via a dedicated flag where needed, not via the
  zero value of the option struct.

## 10. Server-version and capability degradation

Some methods are not supported on every forge (e.g., GitHub.com has
no `ServerVersion` endpoint; Forgejo v15.0.1 has no
`GetWorkflowRunLogs`). The contract:

- Unsupported methods return an `exitcode.Error` with code
  `NotImplemented` and a human-readable message naming the forge
  and the missing capability.
- Callers branch on `NotImplemented` to offer a fallback (a `gaia
  whoami` user-string, the run's `html_url`, etc.) — never to retry.
- A method that works on some forge versions but not others is
  documented as such on its per-method docstring.

## 11. Interface composition (resource ports)

`Provider` is not a flat 50-method interface — it is a **composition of
per-resource ports** declared in `core/provider/ports.go`:

```go
type Provider interface {
    IdentityOps          // Whoami, ServerVersion
    IssueOps             // List/Get/Create/EditIssue
    IssueDependencyOps   // blocker/blocks graph (NotImplemented on GitHub)
    CommentOps           // ListComments + issue/PR comment writes
    PullRequestOps       // PR reads/writes + GetCommitStatus
    SearchOps            // Search
    LabelOps             // List/Create/Edit/DeleteLabel
    ReleaseOps           // releases + assets
    PackageOps           // owner-scoped registry
    WikiOps              // wiki pages
    WebhookOps           // webhooks + deliveries
    ActionsOps           // workflow runs
    MilestoneOps         // milestones + issue roll-up
}
```

**Why (ADR 0001 §Decision criterion 2):** a consumer that needs one
resource depends on the narrow port, not the whole surface. A CLI label
handler takes `provider.LabelOps` (4 methods); a chain step that lists
issues takes `provider.IssueOps`; a test for one resource implements one
port instead of stubbing 45 unrelated methods. The wide `Provider` is
unchanged for callers that genuinely need everything
(`forgebuilder.Build`, the MCP tool dispatcher, the chain orchestrator),
and any `Provider` value still satisfies every port, so existing call
sites keep compiling.

The split is **consumer-facing only** — `core/forgejo` and `core/github`
stay monolithic and satisfy `Provider` by implementing every method.

## 12. Adding a new method

When extending `Provider`:

1. Add the method to the relevant resource port in
   `core/provider/ports.go` (or add a new `XxxOps` port and embed it in
   `Provider`) with a docstring covering inputs, outputs, error modes,
   and any contract deviations from this document (preferably none).
2. Implement on both `core/forgejo` and `core/github`. A missing
   implementation must return `exitcode.NotImplemented` with the
   forge name.
3. Add the parity test under the shared contract suite (TODO: see
   #__) so both forges run through the same scenarios.
4. Update `docs/agent-guide.md` and the coverage list in `CLAUDE.md`.
5. Add the bench measurement to `bench/dogfood-<resource>.md`.

A method that only one forge supports is acceptable, but the
contract — return value, error mode, idempotency — must be the same
shape across both, with the unsupported forge returning
`NotImplemented` rather than a wrong-shape stub.

## 13. What is NOT in the contract

The following are implementation details, not contract:

- HTTP wire format, header values, `User-Agent` string.
- Number of upstream round-trips per Provider call (e.g.
  `GetPullRequest` may issue 1, 2, or 3 calls depending on
  `WithComments` and CI status).
- Internal cache keys, TTLs, or eviction policy.
- Retry backoff timing (the *fact* of one GET retry is contract;
  the 500ms is not).
- The forge's REST URL shape — callers depend on the trimmed
  `types` values, not on URLs.

Callers that depend on any of these are coupled past the interface.
Implementations are free to change them.
