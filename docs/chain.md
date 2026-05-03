# gaia chain — step chaining for agents

Chains describe a multi-step workflow once and let gaia run it in
one CLI invocation. The point: cut agent round-trip count. Today
"open PR → wait for CI → merge" is 5+ agent turns; with a chain it's
one `gaia chain run` call returning the final state in a single
envelope.

This is the v0.x phase-A interface. Linear chains only — no
parallel, no for-each, no chain composition yet. See
[`#112`](https://github.com/stewartbrothers/gaia/issues/112)
for the broader design + phasing.

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

## Limitations (Phase A)

- **Saved chains** in `.gaia/chains/<name>.yaml` (Phase B): today
  every invocation passes `--chain-file <path>`.
- **Named chain composition** (Phase B): one chain calling another
  saved chain as a step.
- **Parallel steps** (Phase C): `parallel:` block + `for_each`,
  for "5 comments at once" patterns.
- **Retries / conditionals** (Phase C): no `if-then` in the step
  grammar; chain stops at first failure.

Tracked under [#112](https://github.com/stewartbrothers/gaia/issues/112).
