# Exit codes

`gaia` uses a small, stable set of exit codes so agents can branch on
the *kind* of failure without parsing stderr. The full table is below;
the constants live in `core/exitcode`.

| Code | Constant          | Meaning                                          |
|------|-------------------|--------------------------------------------------|
| 0    | `OK`              | Operation succeeded.                             |
| 1    | `Generic`         | Unexpected failure; check stderr for detail.     |
| 2    | `Usage`           | The request shape was wrong (400, 422, bad flag).|
| 3    | `NotFound`        | The named issue / PR / repo does not exist.     |
| 4    | `Auth`            | Auth missing or rejected (401, 403).             |
| 5    | `RateLimit`       | Rate-limited by the upstream (429).              |
| 6    | `Network`         | Transient infrastructure failure (408, 5xx, dial).|
| 7    | `MergeConflict`   | PR merge blocked by 409 conflict (head diverged).|
| 8    | `ReviewRequired`  | Branch protection requires reviews not yet present. |
| 9    | `PolicyViolation` | Branch protection blocked the op for another reason (e.g. missing required status check). |
| 10   | `CheckFailed`     | `gaia pr ci-wait` saw a non-flaky CI check fail. |
| 11   | `CheckFlaky`      | `gaia pr ci-wait` timed out while pending OR saw only flaky/retryable failures. |

Codes 7–11 ship with chain Phase B-3 (#112). They let chain `yield_on:`
/ `abort_on:` route on merge / CI / policy outcomes via a structured
condition vocabulary — see `docs/chain.md` for the mapping.

## Why these specific codes

Agents need to know:

- **Did it work?** (`0` vs anything else)
- **Should I retry?** (`5`/`6` yes, `4` only after refreshing auth, the rest no)
- **Is the resource real?** (`3` says no — different from a permission denial)
- **Is the problem the request or the world?** (`2` is on me; `5`/`6` is on them)

That branching matrix is the whole reason the CLI doesn't just return
`0` or `1`. Anything finer-grained (separate codes for 401-vs-403, or
for "PR closed without merge" vs "PR merged") would make the matrix
harder to remember without giving agents enough new signal to use.

## How producers surface a code

Producers (the Forgejo HTTP client, the CLI argument parsers, etc.)
use the helpers in `core/exitcode`:

```go
// Originate a coded error
return exitcode.Errorf(exitcode.NotFound, "issue %d not found in %s/%s", n, owner, repo)

// Wrap an existing error with a code
return exitcode.Wrap(err, exitcode.Network, "fetch issues")

// Translate an HTTP response
return exitcode.Wrap(err, exitcode.FromHTTP(resp.StatusCode), "GET %s", url)
```

`Wrap` preserves the cause so `errors.Is` and `errors.As` walks still
find the underlying error.

## How the CLI translates

`cmd/gaia` (lands with #15) calls `exitcode.Of(err)` on whatever a
subcommand returned and uses the result as the process exit code. A
nil error becomes `0`. Any error that doesn't carry an `*exitcode.Error`
becomes `1` (Generic).

## How agents should branch

```bash
gaia pr view 42
case $? in
  0) ;;                                  # ok
  3) echo "PR doesn't exist" ;;          # NotFound
  4) gaia auth refresh ;;                # Auth
  5) sleep 60 && retry ;;                # RateLimit
  6) sleep 5 && retry ;;                 # Network — short backoff
  *) echo "unexpected"; exit 1 ;;        # Usage / Generic
esac
```

## Wire stability

Existing codes never change value. New codes append. A breaking change
to this convention bumps `core/types.SchemaVersion` and is called out
in the release notes for the version that introduces it.
