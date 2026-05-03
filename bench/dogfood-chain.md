# Chain primitives baseline

Measured against `github.com/stewartbrothers/gaia`.

## Phase B-3: pr-create-and-land canned chain

`make dogfood-chain`, PR #75 shape, 5-poll CI window
(2026-05-03).

### Without chain (multi-call sequence)

| Step                                     | Bytes  | ≈Tokens |
|------------------------------------------|--------|---------|
| `gaia pr create` response                |  4 255 |   1 063 |
| `gaia pr view <n> --with-ci` × 5 polls   | 21 935 |   5 483 |
| `gaia pr merge <n> --method squash`      |     30 |       7 |
| **Total**                                | **26 220** | **6 555** |

### With chain (single envelope)

| Component                                | Bytes | ≈Tokens |
|------------------------------------------|-------|---------|
| chain metadata + 3 step records          |   600 |     150 |
| `captured.pr` (PR data subtree)          | 4 255 |   1 063 |
| **Total**                                | **4 855** | **1 213** |

### Reduction

| Metric                       | Without chain | With chain | Reduction |
|------------------------------|---------------|------------|-----------|
| Bytes read by agent          | 26 220        | 4 855      | **81%**   |
| Approx. tokens               | 6 555         | 1 213      | **81%**   |
| Tool turns                   | 7             | 1          | **86%**   |

The byte reduction comes mostly from collapsing the 5-poll window
into the chain runner's internal poll (which never reaches the
agent's context). The tool-turn reduction (7 → 1) is what moves
the user-visible recovery time when the chain yields and resumes.

## Phase C: parallel + for_each (#149)

Measured during PR #155, "comment on 5 issues concurrently"
shape.

| Metric           | Sequential (5 calls) | Parallel chain | Reduction |
|------------------|----------------------|----------------|-----------|
| Tool turns       | 5                    | 1              | 80%       |
| Wall-clock       | ~2.0 s               | ~0.4 s         | 80%       |
| Bytes            | ~1 000               | ~600           | 40%       |
| Approx. tokens   | ~250                 | ~150           | 40%       |

Latency reduction matters more than byte reduction for fan-out
patterns — the wall-clock win compounds with the concurrent fan
size. For the `open-and-land` chain composition shape, turn
reduction is 66%; the bigger win is reasoning-step delta (one
envelope vs three).

## Caveats

- **Numbers track this repo's PR shape.** A repo with longer PR
  bodies / more labels / more CI checks produces bigger envelopes
  on both sides; ratios stay similar.
- **Chain envelope grows on yield.** A yielded chain carries the
  resume token, yield reason, and yield payload — adds ~200 bytes
  vs. success. Still a small fraction of the without-chain baseline.
- **Without-chain grows on flake retry.** A flaky check that
  forces 3 polling rounds triples the per-poll bytes; with-chain
  costs ~0 extra (the polls happen inside `gaia pr ci-wait`, not
  as MCP turns).
- **Chain envelopes are stable.** The 5-poll baseline is a real
  number; the chain envelope is one number per shape regardless
  of how long the chain ran. So the win compounds with longer CI
  windows and larger fan-out.

## Reproducing

```bash
make build
make dogfood-chain                # live forge
DOGFOOD_FAKE=1 make dogfood-chain # offline harness
```

The script is intentionally cheap — no extra deps, no tokenizer —
so anyone can verify the numbers before claiming a different
ratio.
