# Milestones baseline

No measured rows yet. The Gerwood/gaia repo has no milestones
configured at the time of writing (we triage exclusively via
labels), so live numbers aren't reproducible. The milestone
commands (`gaia milestone list`, `view`, `create`, `edit`, `delete`,
`issues`) are wired through `scripts/dogfood-compare.sh` — re-run
the harness once a repo on the test forge has at least one open
milestone with a few issues attached.

Expected ratios (from the trim-shape analysis, not measured):

- **milestone list** ~0.35–0.45× — gaia drops Forgejo's `creator`
  object (avatar URL + 6 user-record fields), `id` on the list
  shape leaves the wire (we keep it on the typed value for PATCH/
  DELETE), and the `description` field is omitted from list-shape
  responses (agents fetch one via `view` when they need the full
  context). Raw response keeps `html_url`, `repository_id`, plus
  the verbose creator block per row.
- **milestone view** ~0.45–0.55× — single-record trim follows the
  same drop pattern. `gaia milestone view <id>` keeps title,
  description, state, due/closed/created/updated, and the
  open_issues/closed_issues rollup — everything an agent actually
  needs to plan around a sprint. Raw drops out the URL fields,
  creator metadata, and Forgejo's `repository` cross-reference.
- **milestone issues** ~the same trim ratios as `gaia issue list`
  (well-documented in `bench/dogfood-baseline.md`) — the endpoint
  is `/issues?milestones=<id>` on Forgejo, threaded through the
  existing issue trim shape, so a per-milestone issue scan inherits
  whatever shrink ratio the issue list already achieves.

Replace these expectations with measurements once the harness can
run against a repo with active milestones. The milestone-issues
trim is the load-bearing win for sprint planning — an agent that
runs `gaia milestone issues 7 --fields number,title,state` reads
a 30-issue sprint in ~3 KB instead of ~30 KB.
