# Dogfood comparison

gaia produces measurably smaller, agent-shaped responses than raw
forge calls. The headline wins, drawn from
[`bench/dogfood-baseline.md`](../bench/dogfood-baseline.md):

- **`gaia release list`** → **12× smaller** than raw curl on a
  high-asset repo (`cli/cli`); **400× smaller** with
  `--fields tag_name,name,prerelease`.
- **`gaia search`** → **24× smaller** than raw curl. Search results
  collapse to `{kind, number, title, repo}`; raw shape carries full
  repo objects per hit.
- **`gaia issue list` / `gaia pr list`** → **3× smaller** by
  default; **15–35× smaller** with `--fields` projection.
- **`gaia pr view`** → **2.5× smaller** than raw curl, **1.7× smaller**
  than `tea`. `--with-ci` adds an extra round-trip server-side but
  only ~3% to the response size.
- **`gaia wiki search`** → **~25× smaller** than the equivalent
  agent loop of `list pages → fetch each → match locally`. One
  structured response with per-hit snippets vs N WebFetches.
- **`gaia chain run pr-create-and-land`** → **81% byte reduction**
  vs the equivalent multi-call sequence (open PR → poll CI ×5 →
  merge). Tool turns: 7 → 1.
- **SQLite cache** → 0 ms / 0 forge calls on cache hit; conditional
  GET (`If-None-Match`) on TTL miss returns 304 with ~0 bytes.

## Honest losses

- **`gaia pr diff` (full structured)** is **1.6× *bigger*** than raw
  `.diff` text — JSON wrapping per hunk line costs bytes. The win
  is structure (parsed hunks, file metadata); for diffs you almost
  always want `--fields path,status` (35× smaller than raw).

## Where to find the evidence

Per-resource measured baselines live under
[`bench/`](../bench/README.md):

| File | Resource |
|---|---|
| [`dogfood-baseline.md`](../bench/dogfood-baseline.md) | Reads — issue, pr, label, release, search, whoami |
| [`dogfood-streaming.md`](../bench/dogfood-streaming.md) | `--format ndjson` time-to-first-byte |
| [`dogfood-cache.md`](../bench/dogfood-cache.md) | SQLite cache hit / miss / 304 |
| [`dogfood-chain.md`](../bench/dogfood-chain.md) | Chain primitives — fan-out, compose, yield |
| [`dogfood-wiki.md`](../bench/dogfood-wiki.md) | Wiki list / view / search / edit (no live data yet) |
| [`dogfood-packages.md`](../bench/dogfood-packages.md) | Packages list / view / delete / upload (no live data yet) |
| [`dogfood-webhook.md`](../bench/dogfood-webhook.md) | Webhooks list / view / deliveries (no live data yet) |

## Reproducing

```bash
make build
GITEA_TOKEN=<your-token> ./scripts/dogfood-compare.sh
make dogfood-chain                # chain primitives
make cache-bench                  # SQLite cache speedup
```

Token estimates use `bytes / 4` so reproducing doesn't need a
tokenizer. Lower is better — those bytes land in the agent's
context budget.

## When numbers regress

If a gaia command's bytes ratio gets *worse* (relative to raw) at
default settings, that's a regression worth a github-style issue —
the whole product premise is "smaller, agent-shaped output."

## Adding new measurements

**One file per resource.** Adding a row to a shared table is a
merge-conflict generator; per-resource files keep parallel PRs
independent.

- New resource lands → new `bench/dogfood-<resource>.md` file
- Existing resource gets a new command → that resource's own file
  changes only

Estimates (`~140`, `(est.)`) belong in PR descriptions, not in
committed baselines. If a feature has no real forge state to
measure against yet, hold the row until it does — the per-resource
file can carry an "expected ratio" paragraph instead.
