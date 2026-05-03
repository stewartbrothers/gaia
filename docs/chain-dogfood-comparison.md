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
