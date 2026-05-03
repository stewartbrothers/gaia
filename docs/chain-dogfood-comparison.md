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
