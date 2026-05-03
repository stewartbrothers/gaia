# Packages baseline

No measured rows yet. The `Gerwood/gaia` Forgejo instance has no
published packages at the time of writing, so a live side-by-side
isn't reproducible. The packages commands (`gaia packages list`,
`gaia packages view`, `gaia packages delete`, `gaia packages upload`)
are wired through `scripts/dogfood-compare.sh` — re-run the harness
once a package exists and replace this file with the measured table.

Expected ratios (from the trim-shape analysis, not measured):

- A raw Forgejo package record carries `id`, `owner` (full user
  object — login, full_name, email, avatar_url, language,
  last_login, html_url, ...), `repository` (similar bloat),
  `creator` (same), `html_url`, `type`, `name`, `version`,
  `created_at`. ~1 350 bytes per record on a typical user.
- gaia's trimmed `types.Package` is `{type, name, version, owner,
  created_at, size?}` — roughly 130 bytes per record. Owner is
  rendered as the login string, never a nested object.

Expected ratio: ~10× smaller default; ~0.06× with
`--fields type,name,version`. Same pattern as `release` and
`issue` paths. Replace these expectations with measurements once
the harness can run against real packages.
