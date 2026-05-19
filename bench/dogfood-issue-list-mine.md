# Dogfood note: `gaia issue list --assignee @me` (#299)

## Why this file has no bytes-saved numbers

The `@me` sentinel does not change the bytes returned by `gaia issue
list`. It changes the operator (or agent) workflow: pre-feature, the
agent had to chain a `whoami` lookup into the list call.

```bash
# Pre-feature (#299): two commands, plus jq, plus knowing your login.
LOGIN=$(gaia whoami --fields login --format json | jq -r .data.login)
gaia issue list --assignee "$LOGIN" --state open

# Post-feature: one command. gaia issues the /user lookup itself
# (one extra round-trip per invocation, regardless of how many of
# --assignee / --author are "@me").
gaia issue list --assignee @me --state open
```

The dogfood-comparison contract (`scripts/dogfood-compare.sh`)
benchmarks gaia commands against the equivalent raw forge API
response so we can show field-projection wins. That contract still
applies to the underlying `gaia issue list` call — see the row in
`bench/dogfood-baseline.md`. This file only documents the workflow
delta that `@me` adds on top.

## Round-trip cost

The `@me` resolver short-circuits when neither `--assignee` nor
`--author` is `@me`. When at least one of them is `@me`, exactly one
extra GET `/user` call is issued (≈138 bytes back). Both flags being
`@me` still costs only one extra call — the resolver caches the login
for the rest of the command's lifetime.

## CLI shape

```bash
gaia issue list --assignee @me              # issues where I'm an assignee
gaia issue list --author @me                # issues I opened
gaia issue list --assignee @me --author @me # both filters resolved in one /user lookup
gaia issue list --assignee alice            # literal logins unaffected, no /user call
```

## Out of scope (this change)

- `gaia pr list` does not currently expose `--assignee` / `--author`;
  when those flags are added, the same resolver applies trivially.
  File a separate gap when needed.
- A top-level `gaia mine` aggregating issues + PRs in one envelope.
  Bigger UX call — start from issue.
