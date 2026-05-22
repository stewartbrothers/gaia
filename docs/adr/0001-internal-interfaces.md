# ADR 0001 — Internal interfaces: when to introduce one

- **Status:** Accepted
- **Date:** 2026-05-22
- **Deciders:** Gerwood, claude
- **Scope:** `core/`, `internal/`, `cmd/`
- **Related:** `docs/provider-contract.md`, the architectural-review session
  that produced the six candidate refactors in
  `bench/dogfood-comparison.md`-era discussion.

## Context

gaia today contains exactly two `interface` types in non-test code:

- `core/provider.Provider` — the wide (~50-method) contract every forge
  implementation satisfies.
- `core/cache.Cache` — the read-cache contract with two adapters
  (SQLite + Memory).

Everything else is concrete. `forgejo.Client`, `github.Client`, the
auth `Store`, `config.Resolved`, the chain runner, the envelope — all
concrete types. Tests substitute via `httptest.NewServer` or via
`SetXForTest` package-level hooks, not via interface satisfaction.

This minimalism is deliberate and has served gaia well in Phase 1.
The previous heuristic was: *"one adapter means a hypothetical seam;
two adapters means a real one — don't introduce a seam unless
something actually varies across it."* That rule prevents the
`FooService → FooServiceImpl` ceremony common in over-engineered Go
codebases.

The rule starts to bind when we look forward:

- **Phase 2 introduces GitHub** (done) — second forge proved the
  Provider seam was real.
- **Phase 4+ contemplates GitLab, Bitbucket, Azure DevOps**, and
  potentially issue-tracker-only providers like Linear, Jira, or
  this org's sibling project Aikestra. Labels `area:provider-gitlab`,
  `area:provider-bitbucket`, `area:provider-azure` already exist on
  the issue tracker.
- **The chain runner** (`core/chain/run.go`) currently shells out to
  the gaia binary for every forge operation. In-process composition
  is on the roadmap but blocked by the absence of injectable
  per-resource ports.
- **The config layer** is a library of functions; six different sites
  re-run the load-merge-resolve dance per command invocation.
- **The cache** is interface-bound but only the HTTP clients use it.
  Autodetect, doctor, and chain do not.

A strict reading of "two adapters before a seam" lets these scale
gaps persist until they cause acute pain. A looser reading invites
speculation.

## Decision

We restate the rule:

> **Don't invent an interface to mock. Do invent one to bound a
> contract you'll need at scale.**

An interface earns its place when it satisfies *any one* of:

1. **A second real adapter is on the roadmap with a named issue.** Not
   "we might want to swap this someday" — a tracked piece of work
   whose absence today is the only reason the second adapter
   doesn't exist.
2. **Multiple unrelated consumers want a narrow slice** of a wider
   type, and threading the wide type through every call site
   couples consumers to concerns they don't have.
3. **The contract itself is the thing being pinned** — invariants,
   error semantics, concurrency rules — and the interface is the
   document the contract lives on (see `docs/provider-contract.md`
   for `Provider`; the `Cache` package doc for `cache.Cache`).

An interface does *not* earn its place when:

- The only second implementation is a test mock that would otherwise
  use `httptest`.
- It exists to satisfy a layering preference ("the CLI shouldn't
  touch the provider directly") with no behavioural difference at
  the seam.
- It re-exports an existing concrete type's method set unchanged.

The rule applies to **new** interfaces. We do not remove existing
ones (`Provider`, `Cache`) on its basis.

## Consequences

This ADR sanctions the introduction of the following six interfaces.
Each gets its own issue, scoped so the next session picks it up
cold:

1. **`provider.Registry`** — Provider factories self-register in
   `init()`; `forgebuilder.Build` becomes a thin dispatcher.
   Justifying adapter: GitLab provider (next forge to land).
2. **`provider.Capabilities`** — Probe interface for asymmetric
   forges (Linear has no PRs; Aikestra has no wikis). CLI hides
   unsupported subcommands rather than every call returning
   `NotImplemented`.
3. **`core/settings.Settings`** — Single read handle for config +
   credentials + env, loaded once at root command. Replaces the six
   sites that re-run `Load → Merge → Resolve`.
4. **Narrow resource ports** (`IssueOps`, `PullRequestOps`,
   `WebhookOps`, etc.) — sub-interfaces of `Provider`. CLI/MCP
   handlers and future chain step types depend on the slice they
   need, not the full 50 methods.
5. **`chain.StepKind` registry** — Open the closed set of step
   types (`leaf`, `parallel`, `for_each`, `chain`) to registered
   step kinds. Justifying adapter: in-process provider-call step.
6. **`cache.Typed[T]`** — Generic helper over `Cache`. Removes the
   boilerplate that's kept non-HTTP callers (doctor, autodetect,
   chain) from adopting the cache.

Each is independently mergeable; (4) and (5) compose well after (1)
and (3) land.

## Out of scope

This ADR does **not**:

- Refactor existing tests onto interface fakes. `httptest`-based
  testing stays the default at the forge layer.
- Add a generic plugin system. Step-kind registry and provider
  registry are in-process only; no shared-library or RPC plugin
  mechanism is contemplated.
- Pre-commit to the exact shape of any interface. Each linked
  issue carries the design conversation for its specific port.

## Revisiting

Revisit this ADR when:

- A second non-forge provider (Linear, Jira, or Aikestra control
  plane) lands. The owner/repo coupling in current method
  signatures may need a more abstract resource-identifier type;
  that's a future ADR.
- Cross-provider features (cross-forge search, multi-provider
  status dashboards) ship. They will surface whether the narrow
  resource ports want to compose at the consumer side or behind a
  facade.
