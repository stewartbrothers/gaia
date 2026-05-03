# Chain dogfood comparison

Chains collapse multi-call agent workflows into a single envelope.
Headline numbers, drawn from
[`bench/dogfood-chain.md`](../bench/dogfood-chain.md):

- **`pr-create-and-land`** (open PR → wait CI → merge) → **81% byte
  reduction**, **86% tool-turn reduction** (7 → 1) on this repo's
  PR shape with a 5-poll CI window.
- **Parallel fan-out** (5 concurrent issue comments) → **80%
  wall-clock reduction**, **80% tool-turn reduction**, ~40% byte
  reduction. Latency dominates the win for fan-out — concurrent
  execution compounds.
- **Chain composition** (`open-and-land` calling `pr-create-and-land`
  as a step) → 66% turn reduction; bigger reasoning-step delta (one
  envelope vs three).

## Reproducing

```bash
make build
make dogfood-chain                  # live forge
DOGFOOD_FAKE=1 make dogfood-chain   # offline harness
```

The script is intentionally cheap — no extra deps, no tokenizer —
so anyone can verify the numbers before claiming a different ratio.

See [`bench/dogfood-chain.md`](../bench/dogfood-chain.md) for the
per-step measured tables, methodology, and caveats.

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
