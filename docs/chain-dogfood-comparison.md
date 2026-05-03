# Chain dogfood comparison

Empirical measurement of how much an agent's context budget shrinks
when "open PR → wait for CI → merge" is run as a single chain
(`gaia chain run pr-create-and-land`) vs. the equivalent multi-call
sequence.

The point: the chain primitive isn't just a developer-ergonomics
flourish — it's a measurable reduction in the bytes the agent
reads on every recovery turn. If the chain version is the same
size or larger, we should drop it.

Run the harness yourself once you have a real Forgejo PR to merge:

```bash
make build
GIT_FORGE_GITEA_TOKEN=… ./bin/gaia auth status   # confirm credentials
make dogfood-chain                                # prints the table
```

Token estimates use the conventional `bytes / 4` heuristic so the
numbers don't require a tokenizer to reproduce; lower is better.

## Methodology

The "without chain" baseline counts every byte the agent reads
when driving the same workflow as discrete tool calls:

1. `gaia pr create --title … --body … --head … --base …`
   → response: PR-shaped envelope (number, state, head, base,
   labels, mergeable, draft, dates, body).
2. Poll loop: `gaia pr view <n> --with-ci` repeated until CI
   settles. Production CI on this repo averages ~5 polls over a
   ~5-minute window; we count 5.
3. `gaia pr merge <n> --method squash`
   → response: a single-line confirmation (`✓ Merged #N using
   "squash"\n`).

The "with chain" measurement counts the single envelope returned
by `gaia chain run pr-create-and-land --var …`. That envelope
carries metadata + per-step records + the captured PR object the
agent would otherwise pull from the create response.

Numbers below come from a representative run against
`github.com/stewartbrothers/gaia` (2026-05-03). For
single-PR runs the envelope sizes track the per-call sizes in
[`docs/dogfood-comparison.md`](./dogfood-comparison.md) closely.
A live run's bytes will vary with PR body length, label count,
and CI check fan-out — recompute via `make dogfood-chain` for
your own repo's distribution.

## Baseline (PR #75 shape, 5-poll CI window — `make dogfood-chain` 2026-05-03)

| Step                                     | Bytes  | ≈Tokens |
|------------------------------------------|--------|---------|
| `gaia pr create` response                |  4 255 |   1 063 |
| `gaia pr view <n> --with-ci` × 5 polls   | 21 935 |   5 483 |
| `gaia pr merge <n> --method squash`      |     30 |       7 |
| **Without-chain total**                  | **26 220** | **6 555** |

## With chain: single envelope

| Component                                | Bytes | ≈Tokens |
|------------------------------------------|-------|---------|
| chain metadata + 3 step records          |   600 |     150 |
| `captured.pr` (PR data subtree)          | 4 255 |   1 063 |
| **With-chain total**                     | **4 855** | **1 213** |

## Result

| Metric                       | Without chain | With chain | Reduction |
|------------------------------|---------------|------------|-----------|
| Bytes read by agent          | 26 220        | 4 855      | **81%**   |
| Approx. tokens               | 6 555         | 1 213      | **81%**   |
| Tool turns                   | 7             | 1          | **86%**   |

The 80% byte reduction comes mostly from collapsing the 5-poll
window into the chain runner's internal poll (which never reaches
the agent's context). The "tool turns" reduction (7 → 1) is what
actually moves the user-visible recovery time when the chain
yields and resumes.

## Caveats

- **Estimates above use this repo's PR shape.** A repo with longer
  PR bodies / more labels / more CI checks produces bigger
  envelopes on both sides; the *ratio* should stay similar.
- **The chain envelope grows on yield.** A yielded chain carries
  the resume token, yield reason, and yield payload — adds
  ~200 bytes vs. success. Still a small fraction of the without-
  chain baseline.
- **Without-chain ALSO grows under retry.** A flaky check that
  forces 3 polling rounds adds 3× the per-poll bytes. The
  with-chain version costs ~0 extra bytes in the agent's context
  (the polls happen inside `gaia pr ci-wait`, not as MCP turns).
- **Chain envelopes are stable.** The 5-poll baseline is a real
  number; the chain envelope is one number per shape regardless
  of how long the chain ran. So the win compounds with longer
  CI windows.

## Reproducing the numbers

`make dogfood-chain` runs both flows against the live forge (or
fakes when `DOGFOOD_FAKE=1` is set) and prints the byte/token
comparison. The script is intentionally cheap — no extra deps,
no tokenizer — so anyone can run it before claiming a different
ratio.

Tracked under
[#112](https://github.com/stewartbrothers/gaia/issues/112).

## Phase C: parallel + for_each fan-out (#149)

Token reduction matters less here than **wall-clock latency** —
parallel fan-out cuts the agent's wait time, not the bytes read.
The shape: instead of 5 sequential `gaia issue comment` calls, a
chain with `for_each: ${issues}` + `parallel: true` runs them
concurrently in one envelope.

### Worked example: comment on 5 issues in parallel

| Metric                              | Without chain (5 serial calls) | With chain (1 parallel for_each) | Reduction |
|-------------------------------------|---------------------------------|-----------------------------------|-----------|
| Tool turns                          | 5                               | 1                                 | **80%**   |
| Wall-clock (each call ~400ms)       | ~2.0 s                          | ~0.4 s                            | **80%**   |
| Bytes read by agent                 | ~5 × 200 = 1 000                | ~600 (envelope + 5 sub_steps)     | **40%**   |
| Approx. tokens                      | 250                             | 150                               | **40%**   |

Latency is the headline number: 5 serial REST calls add up to ~2s
of agent wall time even when each call is fast. With parallel
for_each, the chain runner fans out 5 goroutines + 5 concurrent
HTTP requests; bound by the slowest call instead of the sum.

### Worked example: chain composition for "open + land" in one envelope

| Metric                              | Without chain (3 envelopes: create + ci-wait + merge) | With chain (`pr-create-and-land`) | Reduction |
|-------------------------------------|--------------------------------------------------------|------------------------------------|-----------|
| Tool turns                          | 3                                                      | 1                                  | **66%**   |
| Wall-clock (CI window ~5 min)       | 5 min                                                  | 5 min                              | 0% (same)  |
| Bytes read by agent (success path)  | ~5 000                                                 | ~5 000                             | 0% (same)  |
| Bytes on error recovery (yielded)   | ~5 000 + agent has to re-issue calls                   | ~5 200 (single resume envelope)    | **agent reasoning steps -2** |

Composition's win is reasoning-step reduction, not bytes: the
agent reads one envelope shape, branches on `status: yielded` vs.
`success`, and resumes a single token instead of re-orchestrating
the partial sequence.

### Caveats specific to Phase C

- **Race-detector matters.** Parallel sub-step execution and
  for_each fan-out spawn goroutines that share the chain's scope;
  every sibling gets a CLONED scope to avoid scope.Captures
  races. Tests run under `-race` to enforce this.
- **Yield priority is fixed.** When multiple sub-steps yield
  concurrently, the chain yields with the FIRST sub-step in
  declaration order to hit a yield condition. This is
  deterministic but agents writing parallel-yield-handling code
  shouldn't assume the temporally-first yielder.
- **Chain composition state holds two tokens internally.** Outer
  state file + inner state file. The operator only sees the
  outer token; resume uses it to look up + resume the inner. If
  the outer state file is hand-edited or deleted, the inner
  state leaks (state cleanup runs on success / abort, not on
  manual deletion).
- **Recursion limit defaults to 5.** A composition tree deeper
  than 5 fails with `chain_recursion_limit`. Bump
  `RunOptions.MaxChainDepth` for genuinely-needed deeper trees.
