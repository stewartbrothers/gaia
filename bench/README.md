# bench/

Harness output and measured baselines. **Not documentation, not a log.**

Files here are produced by `scripts/dogfood-compare.sh` and
`scripts/dogfood-chain.sh` against a real forge, pinning the
byte/token numbers gaia advertises in its README + agent guide.

The hand-curated headline summary lives in
`docs/dogfood-comparison.md` (read first if you want the wins).
Files here are evidence behind the summary.

## Layout (one file per resource)

| File                          | Resource              |
|-------------------------------|-----------------------|
| `dogfood-baseline.md`         | Reads: issue, pr, label, release, search, whoami |
| `dogfood-wiki.md`             | Wiki list/view/search/edit/delete (#108)         |
| `dogfood-packages.md`         | Packages list/view/delete/upload (#107, #122)    |
| `dogfood-webhook.md`          | Webhooks list/view/edit/deliveries (#85)         |
| `dogfood-streaming.md`        | `--format ndjson` time-to-first-byte (#46)       |
| `dogfood-cache.md`            | SQLite cache hit/miss/304 (#42)                  |
| `dogfood-chain.md`            | Chain primitives — fan-out, compose, yield (#112)|

**One file per resource is the rule, not a suggestion.** A shared
table is a merge-conflict generator: every parallel PR appends rows
at the same place. Per-resource files mean a new feature = a new
file (no shared edit) and a measurement refresh = one file changes
(no spurious cross-feature contention).

When a new resource lands, add a new file. Never paste rows into an
existing file unless you're updating that resource's measurements.

## Updating

Re-run the harness when:

- A new gaia command lands (add a new file or row in the resource's
  own file)
- A trim change (provider-side or envelope-side) shifts the bytes
  meaningfully
- The forge state on the test repos drifts enough that ratios
  change (rare; ratios are stable even as raw bytes move)

```bash
make build
GITEA_TOKEN=<your-token> ./scripts/dogfood-compare.sh > bench/dogfood-baseline.md
make dogfood-chain                                    > bench/dogfood-chain.md
# per-resource scripts coming as harness gains target flags;
# until then, edit the per-resource files by hand from a measured
# run and stamp the date.
```

Inspect the diff before committing. If a ratio got *worse*, file an
issue — the product premise is "smaller, agent-shaped output."

## What lives here, what doesn't

**Lives here:**
- Measured rows from a real harness run (commands you can re-run
  against the same target and get the same byte counts within
  forge-state drift).
- The exact target each file measured against (forge URL, repo,
  PR/issue number, date).

**Doesn't live here:**
- Estimated rows (`~140`, `(est.)`) — those go in design notes / PR
  descriptions, not in baselines. If a feature has no real forge
  state to measure against, hold the row until it does.
- Per-call usage logs — those land in `.gaia-usage.jsonl`
  (gitignored, repo-adjacent).
- Hand-curated commentary about why the wins matter — that's
  `docs/dogfood-comparison.md`'s job.
