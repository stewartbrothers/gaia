# gaia chain — step chaining for agents

Chains describe a multi-step workflow once and let gaia run it in
one CLI invocation. The point: cut agent round-trip count. Today
"open PR → wait for CI → merge" is 5+ agent turns; with a chain it's
one `gaia chain run` call returning the final state in a single
envelope.

**Currently shipping:** linear chains (Phase A) + yield/resume with
disk-backed state (Phase B-1) + per-step timeout/retry +
default_yield_on + cleanup: + `--decision modify` (Phase B-2) +
saved chains under `.gaia/chains/<name>.yaml` + the canned
`pr-create-and-land` chain + structured exits on
`gaia pr ci-wait` / `gaia pr merge` (Phase B-3). Linear-only — no
parallel, no for-each, no chain composition yet. See
[`#112`](https://github.com/stewartbrothers/gaia/issues/112)
for the broader design + phasing.

## Subcommands

```
gaia chain run <name>                       # start a saved chain
gaia chain run --chain-file FILE [flags]    # start a chain from a path
gaia chain resume <token> [--decision …]    # pick up a yielded chain
gaia chain list                             # show yielded chains
gaia chain abort <token>                    # discard a yielded chain
```

`gaia chain run <name>` resolves `<name>` in this order (first hit wins):

  1. Literal path — `<name>` contains a `/` or ends in `.yaml`/`.yml`
  2. Project saved chain — `${ProjectRoot}/.gaia/chains/<name>.yaml`
  3. Global saved chain — `~/.config/gaia/chains/<name>.yaml`

`--chain-file <path>` is the explicit form: always a path, bypasses
the saved-chain lookup. When both `--chain-file` and a positional
argument are supplied, `--chain-file` wins.

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
    # Substituted refs are shell-quoted automatically (#135), so don't
    # wrap them in your own "..." — that would put the quote chars
    # inside the resulting argument.
    run: gaia pr create --title ${title} --body ${body} --head ${head} --base ${base}
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

# Phase B-2: chain-level fallback yield list. Applies to steps that
# don't declare their own yield_on.
default_yield_on:
  - <YieldCondition>                 # auth_error, rate_limited, timeout, ...

vars:
  <name>:                            # snake_case or camelCase
    required: <bool>                 # error at startup if not supplied
    default: <string>                # filled in when not supplied

steps:                               # at least one; ordered
  - id: <required, [A-Za-z_][A-Za-z0-9_]*>
    run: <required, shell command>
    capture: <optional, ident>       # save stdout into ${this-name.*}
    yield_on: [<YieldCondition>...]  # pause+resume conditions
    abort_on: [<YieldCondition>...]  # stop conditions
    timeout: <duration>              # Phase B-2; e.g. "30s", "5m", "1h"
    retry:                           # Phase B-2
      max: <int>                     # extra attempts after the first
      delay: <duration>              # wait between attempts
      backoff: <constant|linear|exponential>
    on_failure:
      return:                        # map shipped as Result.Failure on failure
        <key>: <value, with ${} substitution>

# Phase B-2: best-effort steps that run on abort. Same shape as
# steps:; share chain-level vars + captures.
cleanup:
  - id: <required>
    run: <required>
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

### Security: variable substitution semantics

Substituted values are treated as **shell-literal data**, not as
shell tokens. Each `${var}` / `${capture.path}` reference inside a
`run:` string is wrapped in POSIX single quotes before being
spliced into the resolved command line. A hostile var or
attacker-controlled capture cannot inject shell metacharacters
(`;`, `&&`, backticks, `$()`, newlines, …) because the surrounding
single quotes neutralise them.

Concrete example:

```yaml
steps:
  - id: greet
    run: echo Hello, ${name}
```

Run with `--var name="'; rm -rf \$HOME #"` — the resolved line is

```
echo Hello, ''\''; rm -rf $HOME #'
```

i.e. one single-quoted argument that the surrounding `sh -c`
prints verbatim. No deletion happens.

This is a deliberately safe-by-default design (#135). Authors **do
not** need to wrap `${var}` references in their own `"..."` —
doing so puts the surrounding quote characters *inside* the
resulting argument:

```yaml
# WRONG — the title arg literally becomes 'feat: thing'
run: gaia pr create --title "${title}"

# RIGHT — the title arg is feat: thing
run: gaia pr create --title ${title}
```

If a chain genuinely needs to hand a substituted value to a
sub-shell as a script body (rare), the author opts in explicitly:

```yaml
# Operator KNOWS body is intended as shell code; uses an explicit
# nested sh -c so the outer literal isn't shell-quoted.
run: sh -c "${body}"
```

The `on_failure.return` substitution path is **not** shell-quoted —
its values are emitted into the failure envelope as JSON, not
handed to a shell. Captures into a YAML map context keep their raw
form.

### Security: env scrubbing

Chain step children inherit a **scrubbed** environment, not the
gaia process's full env. The allowlist is intentionally short:

| Var | Why it's allowed |
|---|---|
| `PATH` | Required so `sh -c` can find any binary at all |
| `HOME` | `gaia` itself reads `~/.config/gaia/credentials.yaml`; child gaia invocations need it |
| `USER`, `LOGNAME` | Tools like `git` use these; not secret |
| `LANG`, `LC_ALL` | Locale; some tools change output without it |
| `TERM` | Terminal type; CLIs that do colour respect it |

Everything else — `GITEA_TOKEN`, `FORGEJO_TOKEN`, `GH_TOKEN`,
`GITHUB_TOKEN`, `AWS_*`, `GCP_*`, `AZURE_*`, and any other
operator-scope vars — is stripped from the child's view.

Why: combined with the shell-quoting from #135, this closes the
two-step exfiltration path "hostile forge response → shell-injection
→ `env` reads my forge token". Even a future shell-injection
regression cannot escalate to a token leak through the env: the
token isn't in the child's env to read.

If a chain step legitimately needs a forge token (rare — most
forge calls go through `gaia` subcommands, which do their own
credential resolution from `~/.config/gaia/credentials.yaml`), the
correct path is to invoke `gaia` rather than to splice a token
into a `run:` line. `gaia` reads the credential from disk; the
token never appears in argv, env, or stdout.

Per-step env declarations (`env: [GITEA_TOKEN, ...]`) are not
implemented — a chain author who thinks they need that should
file an issue describing the workflow so we can find the
non-token-bearing path. (#140 part 4.)

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
| `check_failed` | exit code 10 (`exitcode.CheckFailed`) — non-flaky CI failure |
| `check_flaky` | exit code 11 (`exitcode.CheckFlaky`) — flaky CI failure or ci-wait timeout |
| `merge_conflict` | exit code 7 (`exitcode.MergeConflict`) — gaia pr merge 409 |
| `review_required` | exit code 8 (`exitcode.ReviewRequired`) — branch protection: missing approvals |
| `policy_violation` | exit code 9 (`exitcode.PolicyViolation`) — branch protection: other block |

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

## Per-step timeout + retry (Phase B-2)

```yaml
- id: brittle
  run: gaia pr ci-wait ${pr.number}
  timeout: 30m              # any time.ParseDuration string
  yield_on: [timeout]       # route timeout-induced failure as yield
  retry:
    max: 3                  # up to 3 retries (4 total attempts)
    delay: 30s              # wait between attempts
    backoff: exponential    # 30s, 60s, 120s, ... (linear / constant also)
```

**Timeout** kills the step's subprocess after the configured wall-
clock duration; the runner emits the step result with
`timed_out: true` and routes the synthetic `timeout` condition
through `yield_on` / `abort_on` / `default_yield_on` like any other
condition.

**Retry** wraps the step in a loop. Up to `max` retries after the
initial attempt, sleeping `delay` between (scaled by backoff).
Final-attempt-only routing means a transient failure that recovers
on retry produces a clean `StepOK` — the chain doesn't yield in the
middle of a retry sequence. The step's `attempts` field records
how many tries it took.

Backoff strategies:

| `backoff:` | Sleep schedule (after attempt N) |
|---|---|
| `constant` | `delay`, `delay`, `delay`, ... |
| `linear` | `delay`, `2×delay`, `3×delay`, ... |
| `exponential` (default) | `delay`, `2×delay`, `4×delay`, ... |

## Chain-level `default_yield_on` (Phase B-2)

```yaml
default_yield_on: [rate_limited, timeout]
steps:
  - id: a
    run: gaia ...
    # no per-step yield_on — the chain default applies
  - id: b
    run: gaia ...
    yield_on: [auth_error]   # explicit per-step list — chain default does NOT apply here
```

The chain default applies only when a step has an empty `yield_on`.
A non-empty per-step list is the authoritative whitelist for that
step (so an operator who explicitly opted out of a default can
do so by listing the conditions they DO want, leaving out the
default ones). `abort_on` is unaffected — there's no
`default_abort_on` (yet).

## `cleanup:` block (Phase B-2)

```yaml
steps:
  - id: open
    run: gaia pr create ...
    capture: pr
  - id: merge
    run: gaia pr merge ${pr.number}
    abort_on: [merge_conflict]

cleanup:
  - id: close-orphan
    run: gaia pr close ${pr.number} --comment "abandoned by chain"
```

Cleanup steps run **only on abort**, in declared order, on a
best-effort basis. A failing cleanup step is recorded but doesn't
halt later cleanup steps — the goal is "clean up as much as
possible." Cleanup steps share the chain's resolved scope (vars +
captures from the main run) so they can reference whatever the main
chain produced.

The aborted Result envelope carries:

```json
{
  "data": {
    "status": "aborted",
    "abort_reason": "merge_conflict",
    "cleanup_results": [
      { "id": "close-orphan", "status": "ok", "duration_ms": 412, ... }
    ],
    ...
  }
}
```

## `--decision modify` (Phase B-2)

When a chain yields, the agent can re-run the yielded step with
modified vars instead of just continuing or aborting:

```bash
gaia chain resume <token> \
  --decision modify \
  --modify-step wait-checks \
  --modify-vars timeout=10m,branch=main
```

`--modify-step` must match the yielded step's id; only the yielded
step is editable mid-flight (other steps' args are part of the
frozen chain spec). `--modify-vars` accepts the same `key=value`
shape as the original `--var` flag (repeatable; comma-separated;
`=` splits on first occurrence).

The modified vars are persisted to the resumed state so a re-yield
preserves them — an agent that fixes a value once doesn't have to
re-pass it on every retry.

## Saved chains (Phase B-3)

```
.gaia/chains/<name>.yaml          # project-local — committable
~/.config/gaia/chains/<name>.yaml # global, per-user
```

Run by name:

```bash
gaia chain run pr-create-and-land --var title=… --var body=… --var head=…
```

The project-local layer is the "team agreed on this chain, ship
it with the repo" path; the global layer is "every project on
this laptop should be able to call ship-prod-tag." Resolution is
project → global, so a project file shadows the global one when
both exist.

### Canned chain: `pr-create-and-land`

Shipped at `.gaia/chains/pr-create-and-land.yaml`. Opens a PR,
waits for CI to settle, merges. Routing:

| Step          | Yield on                                      | Abort on        |
|---------------|-----------------------------------------------|-----------------|
| `open`        | (default failure → `pr-create-failed`)        | —               |
| `wait-checks` | `rate_limited`, `check_flaky`, `timeout`      | `check_failed`  |
| `merge`       | `merge_conflict`, `review_required`, `rate_limited` | —         |

`check_failed` is intentionally `abort_on:` rather than `yield_on:` —
a real test break should never silently retry. `check_flaky` /
`merge_conflict` / `review_required` → yield lets the agent re-trigger
CI / push a rebase / wait for an approval and resume.

Token-budget evidence — see
[`docs/chain-dogfood-comparison.md`](./chain-dogfood-comparison.md).
Short version: the chain envelope is ~1/3 the bytes of the equivalent
`gaia pr create` → poll → `gaia pr merge` agent flow.

## `gaia pr ci-wait` (Phase B-3)

```bash
gaia pr ci-wait <number> [--timeout 30m] [--interval 10s] [--flaky-marker LABEL]
```

Polls the PR's commit-status endpoint until checks settle or
`--timeout` expires. Designed for chain consumption — exits with
structured codes the chain runtime maps to yield/abort conditions:

| Exit | Constant     | Condition         | Typical chain placement       |
|------|--------------|-------------------|-------------------------------|
| 0    | OK           | —                 | success                       |
| 10   | CheckFailed  | `check_failed`    | `abort_on: [check_failed]`    |
| 11   | CheckFlaky   | `check_flaky`     | `yield_on: [check_flaky]`     |

Flakiness classifier: a check name matching `flaky` / `attempt N` /
`retry` / `rerun` (case-insensitive) is "flaky"; everything else is
"hard." `--flaky-marker` adds operator-specified substrings.
**Mixed flaky+real failures classify as `CheckFailed`** — we never
silently demote a real failure.

Caveats:

- The flakiness regex is a heuristic. A team that names checks
  `tests-attempt-1` (intending to mean "first attempt of one") would
  trip it; rename or add `--flaky-marker` to expand the matcher
  rather than working around it.
- We don't currently track per-check history across polls (so the
  "pending → failure → success" recovery heuristic mentioned in the
  alt-design doc isn't implemented). The retry-marker name pattern
  catches the common case; complex flakiness needs a follow-up.

## `gaia pr merge` structured exits (Phase B-3)

`gaia pr merge` now classifies the upstream's "can't merge today"
responses into chain-routable codes:

| HTTP        | Body cue                          | Exit | Chain condition    |
|-------------|-----------------------------------|------|--------------------|
| 409         | (any)                             | 7    | `merge_conflict`   |
| 405         | mentions reviews / approvals      | 8    | `review_required`  |
| 405         | other (failed checks, lock, etc.) | 9    | `policy_violation` |

The 405 vs review/policy split is inherently a body-text sniff because
both Forgejo and GitHub return the same status for "needs approvals"
and "needs passing checks." Marker lists in the provider source are
intentionally narrow on the review side — false positives would push
`abort_on: [policy_violation]` to never fire when it should.

## Limitations (current)

- **Parallel steps + for_each** (Phase C): no parallel fan-out
  for "5 comments at once" patterns.
- **Named chain composition** (Phase C): one chain calling
  another saved chain as a step.

Tracked under [#112](https://github.com/stewartbrothers/gaia/issues/112).
