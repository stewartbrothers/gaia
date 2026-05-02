# Testing strategy

gaia tests run in two tiers per provider. Both tiers gate every PR
and every push to `main` via `make cover`.

## Tier 1: hand-rolled httptest (per-method)

Each provider method has a partner `_test.go` file in
`core/forgejo/` or `core/github/` that spins up an `httptest.Server`
with handcrafted JSON responses. These tests pin:

- the request shape (URL path, method, query params, body)
- the trim contract (which fields land in the `core/types` value)
- error mapping (404 → `exitcode.NotFound`, 401 → `Auth`, etc.)
- option-struct semantics (`Draft *bool`, `omitempty` tags)

This is the bulk of the suite — fast (sub-second), no network, no
auth, fully deterministic.

## Tier 2: recorded fixtures (GitHub)

`core/github/testdata/fixtures/` holds 8 captured api.github.com
responses from the public `cli/cli` repo. The fixture-replay
harness (`core/github/fixturetest_test.go`) provides
`fixtureServer(t, routes)` returning an `httptest.Server` that
replays the named fixture as the response body.

These tests pin the same trim contract Tier 1 does — but against
real, in-the-wild GitHub responses — so the suite catches drift
between hand-rolled fixtures and what GitHub actually sends. If a
GitHub API version bump silently changes a field name or shape,
Tier 2 fails loudly during the next `make cover`.

### When to re-record

Run `./scripts/record-gh-fixtures.sh` when any of these happens:

- A new method-under-test wants a recorded fixture.
- The `X-GitHub-Api-Version` pinned in `core/github/client.go`
  bumps.
- A Tier 2 test starts failing in a way that suggests stale data
  (e.g., the captured PR is now closed, the captured release has
  been deleted upstream).

The script accepts `GH_TOKEN=ghp_...` for higher rate limits but
runs unauthenticated against the public cli/cli repo by default.

### Why path-prefix routing in the harness

Some methods fan out to multiple endpoints —
`GetPullRequest(WithCISummary)` fetches both `/pulls/{n}` and
`/commits/{sha}/check-runs`; `GetIssue(WithComments)` fetches
`/issues/{n}` and `/issues/{n}/comments`. `fixtureServer` accepts
a `map[string]string` of path-prefix → fixture-name, longest
prefix wins, `""` is the fallback. This keeps fixture files
atomic (one captured response per file) while letting one test
combine several into a multi-call scenario.

## Tier 2 for Forgejo?

Not yet — the Forgejo provider runs against a self-hosted instance
under our control, so wire-shape drift between Forgejo releases is
something we'd notice via `make cover` against an upgraded
instance. If a future Forgejo upgrade silently changes a wire
shape, the symptom is the existing Tier 1 tests would still pass
(hand-rolled fixtures still match the *previous* shape) but live
calls would fail. At that point a Tier 2 fixture set for Forgejo
becomes worth the maintenance.

## Running

```bash
make cover            # all tiers, both providers, with coverage summary
go test ./core/github/... -run FromFixture -v   # just the recorded-fixture tier
```

The recorded-fixture tier adds <1s to the total suite time.
