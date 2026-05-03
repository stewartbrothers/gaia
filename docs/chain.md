# gaia chain — step chaining for agents

Chains describe a multi-step workflow once and let gaia run it in
one CLI invocation. The point: cut agent round-trip count. Today
"open PR → wait for CI → merge" is 5+ agent turns; with a chain it's
one `gaia chain run` call returning the final state in a single
envelope.

**Currently shipping:** linear chains (Phase A) + yield/resume with
disk-backed state (Phase B-1). Linear-only — no parallel, no
for-each, no chain composition yet. See
[`#112`](https://github.com/stewartbrothers/gaia/issues/112)
for the broader design + phasing.

## Subcommands

```
gaia chain run --chain-file FILE [flags]    # start a chain
gaia chain resume <token> [--decision …]    # pick up a yielded chain
gaia chain list                             # show yielded chains
gaia chain abort <token>                    # discard a yielded chain
```

## Quick example

`pr-and-merge.yaml`:

```yaml
name: pr-and-merge
description: Open a PR with the supplied title/body and merge once it's clean.

vars:
  title:
    required: true
  body:
    required: true
  head:
    required: true
  base:
    default: main

steps:
  - id: open
    run: gaia pr create --title "${title}" --body "${body}" --head "${head}" --base "${base}"
    capture: pr
    on_failure:
      return:
        reason: pr-create-failed
        error: "${error.message}"

  - id: merge
    run: gaia pr merge ${pr.number} --method squash
    on_failure:
      return:
        reason: merge-failed
        pr_number: "${pr.number}"
        error: "${error.message}"
```

Run it:

```bash
gaia chain run --chain-file pr-and-merge.yaml \
  --var title="feat: ship the thing" \
  --var body="full description" \
  --var head=feature/thing
```

Result envelope (success):

```json
{
  "data": {
    "chain": "pr-and-merge",
    "status": "success",
    "steps": [
      { "id": "open",  "status": "ok", "duration_ms": 412, "stdout": "..." },
      { "id": "merge", "status": "ok", "duration_ms": 287, "stdout": "..." }
    ],
    "captured": {
      "pr": { "number": 42, "title": "...", "state": "merged" }
    },
    "duration_ms": 712
  }
}
```

Result envelope (failure during merge step):

```json
{
  "data": {
    "chain": "pr-and-merge",
    "status": "failure",
    "failed_step": "merge",
    "failure": {
      "reason": "merge-failed",
      "pr_number": "42",
      "error": "GET /repos/.../pulls/42: HTTP 405: ..."
    },
    "steps": [
      { "id": "open",  "status": "ok",     "duration_ms": 412 },
      { "id": "merge", "status": "failed", "duration_ms": 53, "exit_code": 1 }
    ]
  }
}
```

Exit codes: `0` for success, `1` for chain failure, `2` for usage
error (bad flags, var validation).

## YAML schema

```yaml
name: <required, freeform>           # used in result envelope + logs
description: <optional, freeform>

vars:
  <name>:                            # snake_case or camelCase
    required: <bool>                 # error at startup if not supplied
    default: <string>                # filled in when not supplied

steps:                               # at least one; ordered
  - id: <required, [A-Za-z_][A-Za-z0-9_]*>
    run: <required, shell command>
    capture: <optional, ident>       # save stdout into ${this-name.*}
    on_failure:
      return:                        # map shipped as Result.Failure on failure
        <key>: <value, with ${} substitution>
```

## Variable substitution

`${name}` and `${name.path.to.field}` references work in:

- step `run` lines (after parsing, before execution)
- `on_failure.return` map values (recursively, on failure)

Resolution:

| Pattern | Resolves to |
|---|---|
| `${title}` | `vars.title` (chain input from `--var`) |
| `${pr}` (no dot) | `vars.pr` if set, else captures.pr (whole object as JSON) |
| `${pr.number}` | `captures.pr.number` (descended via map keys) |
| `${pr.head.ref}` | `captures.pr.head.ref` |
| `${error.message}` | inside `on_failure.return` only — synthesized |

Bare `${name}` favors a `vars` entry over a `captures` entry. The
dotted form (`${name.path}`) always reads from captures.

Unresolved references in a `run:` line are a hard failure for that
step (`reason: unresolved_variables`). Unresolved references in
`on_failure.return` ship as the literal `${...}` placeholder so the
operator sees what wasn't bound.

### Stringification

JSON-decoded values render for shell substitution as:

| Type | Renders as |
|---|---|
| string | the string verbatim |
| number (integer-valued) | `42` |
| number (fractional) | `1.5` |
| bool | `true` / `false` |
| null | empty string |
| object | compact JSON (`{"k":"v"}`) |
| array | compact JSON (`[1,2,3]`) |

### `${error}` inside on_failure

When a step fails and `on_failure.return` is being substituted, gaia
synthesizes a temporary `${error}` capture:

```yaml
error:
  message: <stderr tail of the failing step>
  stdout:  <stdout tail of the failing step>
  step:    <the failing step id>
```

So `error: "${error.message}"` surfaces the failing process's stderr
to the agent.

## Capture semantics

Setting `capture: pr` on a step makes its stdout available as
`${pr.*}` in later steps. Three forms parsed automatically:

| stdout looks like | captured as |
|---|---|
| gaia envelope (`{"schema_version":"1.0","data":{...}}`) | `data` subtree only — agents work with the trimmed shape |
| Other JSON (`{"foo":"bar"}` or `[...]`) | the whole parsed value |
| Anything else | the trimmed stdout string |

Captures are NOT truncated — agents read the full payload. Step
stdout/stderr in the result envelope ARE truncated to 4 KB to keep
the envelope small.

## Failure behavior

A chain stops at the first failing step. Failure means:

- non-zero process exit code, OR
- the process couldn't be started (e.g., `command not found`), OR
- the `run:` line has unresolved variable references

When a step fails:

1. The step's record gets `status: failed` + the exit code.
2. If `on_failure.return` is set, that map (with substitution applied)
   becomes `Result.Failure`.
3. Otherwise `Result.Failure` is a default `{reason, step, stderr}`
   shape.
4. No further steps run.
5. The CLI exits with code 1.

## Dry-run

```bash
gaia chain run --chain-file ci.yaml --var name=value --dry-run
```

Substitutes vars, renders the resolved `run:` line per step, marks
each step `status: skipped`, doesn't execute anything. Useful for
verifying variable bindings + glob patterns + on_failure shapes
before a real run.

Substitution against captures isn't possible in dry-run (no step has
executed), so `${pr.number}` etc. show as literal placeholders.

## Verbose mode

```bash
gaia chain run --chain-file ci.yaml --verbose
```

Streams `[step-id] ok|failed in Nms` lines to stderr while the chain
runs. The final envelope still goes to stdout. Useful for long
chains where the operator wants progress visibility.

## Tips for writing chains

- **Capture only what later steps need.** A capture pulls the
  step's stdout into the result envelope; if nothing references
  `${cap.*}`, drop the `capture:` to keep the envelope small.
- **Use `--dry-run` in CI** as a lint pass — guarantees the chain
  YAML parses + all `${vars}` resolve before the real run starts
  burning time.
- **Quote shell-special values.** `--var msg="contains spaces"` →
  `${msg}` substitutes literally; downstream `echo "${msg}"`
  wraps in shell quotes. If the value contains `&`/`|`/`;`/etc.
  the operator must shell-quote inside the YAML's `run:` value.
- **`on_failure` lets you give the agent structured failure
  data.** Better than relying on the agent to parse stderr.
  Always ship a `reason:` field at minimum so dispatch is trivial.

## Yield + resume (Phase B-1)

A step can declare conditions that pause the chain instead of
failing it. The runner saves state to disk, returns a
`resume_token` to the agent, and the agent picks up later via
`gaia chain resume`.

### Yield-condition vocabulary

Fixed enum (named tokens; agents branch on them without parsing
free text):

| Condition | Maps from |
|---|---|
| `auth_error` | exit code 4 (`exitcode.Auth`) |
| `not_found` | exit code 3 (`exitcode.NotFound`) |
| `rate_limited` | exit code 5 (`exitcode.RateLimit`) |
| `timeout` | step exceeded its `timeout:` (Phase B-2) |
| `unknown_error` | exit code 1 or any unmapped non-zero |
| `check_failed` | non-flaky CI failure (Phase B-3+) |
| `check_flaky` | flaky CI failure (Phase B-3+) |
| `merge_conflict` | gaia pr merge 409 (Phase B-3+) |
| `review_required` | branch protection blocked merge (Phase B-3+) |
| `policy_violation` | other policy block (Phase B-3+) |

### Step grammar additions

```yaml
- id: brittle
  run: gaia pr ci-wait ${pr.number}
  yield_on:
    - rate_limited
    - timeout
  abort_on:
    - auth_error      # if creds break, no point retrying
```

Routing on step failure:

1. Exit code → condition (via `MapExitCode` table above).
2. If condition is in `yield_on` → status: yielded, write state, agent resumes later.
3. If condition is in `abort_on` → status: aborted, no resume.
4. Otherwise → existing failure flow (`on_failure: { return: ... }` or default `{ reason, step, stderr }`).

Same condition can't appear in both `yield_on` and `abort_on` for
the same step — parser rejects.

### Yield envelope

```json
{
  "data": {
    "chain": "pr-and-merge",
    "status": "yielded",
    "resume_token": "8fe3eb840f45c927dfd41557a2cb0310",
    "yield_reason": "rate_limited",
    "yield_payload": {
      "step": "wait-checks",
      "exit_code": 5,
      "stderr": "...",
      "stdout": "..."
    },
    "remaining_steps": ["merge"],
    "steps": [...],
    "captured": {...}
  }
}
```

### Resume

```bash
# Default: re-run the yielded step (after fixing the underlying cause).
gaia chain resume 8fe3eb840f45c927dfd41557a2cb0310

# Discard the chain instead.
gaia chain resume 8fe3eb840f45c927dfd41557a2cb0310 --decision abort
# (or: gaia chain abort <token>)
```

If the underlying cause is still broken, the chain yields again
with a **new** token. The old token is cleaned up automatically.

`gaia chain list` shows currently-yielded chains:

```
$ gaia chain list --format pretty
bb0165a7ad242a189e99dd6e59f53628  2026-05-03T11:15:34+10:00
```

### State location + lifecycle

Yielded state lives at:

```
$XDG_STATE_HOME/gaia/chains/<token>.yaml
# or  ~/.local/state/gaia/chains/<token>.yaml when XDG isn't set
```

Mode `0600`, parent `0700`. Same path family as `~/.config/gaia/`.
**Local-only** — no daemon, no cross-machine resume. Same UX
pattern as `git rebase --continue`'s `.git/rebase-merge/`.

Files are cleaned up automatically:

- On resume success / failure / abort → state file deleted
- On chain command startup → files older than 24h removed
  (opportunistic, no cron)

### When to use yield vs abort vs default-fail

| Scenario | Declaration |
|---|---|
| Transient that the agent might fix mid-flight (rate limit, flaky check, push fix commit) | `yield_on:` |
| Hard stop where retrying makes no sense (creds expired, policy block) | `abort_on:` |
| Anything else | leave undeclared — falls through to `on_failure` (Phase A) |

Default-fail (Phase A) is still the right answer for chains where
the agent isn't expected to recover — single-shot CI lints,
deterministic tooling pipelines, etc.

## Limitations (current)

- **Saved chains** in `.gaia/chains/<name>.yaml` (Phase B-3): today
  every invocation passes `--chain-file <path>`.
- **`gaia chain resume --decision modify`** (Phase B-2): change
  the yielded step's args before re-running. Today only
  `continue` and `abort` are supported.
- **Per-step `timeout` + `retry`** (Phase B-2): currently any
  exit-code-based yield/abort works, but timeout-driven yields
  need the runner to enforce per-step deadlines.
- **Chain-level `default_yield_on`** (Phase B-2): operators
  declare `yield_on` per step today.
- **`cleanup:` block on abort** (Phase B-2): no automatic cleanup
  of partially-completed work.
- **Parallel steps + for_each** (Phase C): no parallel fan-out
  for "5 comments at once" patterns.
- **Named chain composition** (Phase C): one chain calling
  another saved chain as a step.

Tracked under [#112](https://github.com/stewartbrothers/gaia/issues/112).
