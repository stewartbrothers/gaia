# Webhooks baseline

No measured rows yet. The `Gerwood/gaia` repo has no webhooks
configured at the time of writing, so live numbers aren't
reproducible. The webhook commands (`gaia webhook list`,
`gaia webhook view`, `gaia webhook deliveries`,
`gaia webhook redeliver`, `gaia webhook test`) are wired through
`scripts/dogfood-compare.sh` — re-run the harness once at least
one webhook with a few deliveries exists.

Expected ratios (from the trim-shape analysis, not measured):

- **webhook list** ~0.30× — gaia's trimmed `Webhook` drops the
  full `config` object's secret, headers, and the verbose
  `last_response` summary on each entry.
- **webhook view** ~0.40× — single-record trim; same drop pattern.
- **webhook deliveries (list shape)** ~0.06× of raw — this is the
  **headline win**. Raw Forgejo responses inline every delivery's
  full request + response bodies (routinely 1.5–5 KB *per delivery*
  on a `push` event with full commit list). gaia's list shape
  carries only the summary (id, event, status_code, duration_ms,
  delivered_at, redelivery flag); fetch one detail with
  `gaia webhook deliveries <id> --get N` when you actually need
  the body.

Replace these expectations with measurements once the harness can
run against a webhook with deliveries. The deliveries-list win is
load-bearing for #85 dogfood — it's the reason webhook ops
become tractable for an agent.
