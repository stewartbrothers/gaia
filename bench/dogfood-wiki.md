# Wiki baseline

No measured rows yet. The test wiki is empty at the time
of writing, so live harness numbers aren't reproducible. The wiki
read commands (`gaia wiki list`, `gaia wiki view`, `gaia wiki search`)
are wired through `scripts/dogfood-compare.sh` — re-run the harness
against a non-empty wiki and replace this file with the measured
table.

Expected ratios (from the trim-shape analysis, not measured):

- **wiki view** ~0.55× — Forgejo's `WikiPage` carries
  `content_base64` + URL/HTML render fields gaia drops; the body
  itself dominates the response on prose pages so the ratio is
  modest.
- **wiki list** ~0.20× — list responses drop bodies entirely;
  trimmed `WikiPage` is title + path + short SHA + `updated_at`.
- **wiki search** ~25× smaller than the equivalent
  `list pages → fetch each → match locally` agent loop. This is the
  headline win for #108: a single MCP `gaia_wiki_search` call
  collapses N WebFetches into one structured response with
  per-hit snippets.

Pin these as estimates in PR descriptions if you need numbers
before the harness has a non-empty wiki to point at; do not
commit estimates here.
