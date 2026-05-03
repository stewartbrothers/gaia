# Security Policy

Thanks for taking the time to look. gaia stores forge credentials, runs
shell commands as part of `gaia chain`, and exposes a remote MCP server,
so the project takes vulnerability reports seriously. The fastest way to
get a fix shipped is to follow the process below.

## Threat model

gaia handles forge access tokens (Forgejo / GitHub PATs), which can
read and write on the operator's behalf. A vulnerability in gaia is
worth treating as if it were a vulnerability that exposes those
tokens. Specifically in scope:

- **Token leakage** — anything that writes a forge token to a log,
  argv, environment dump, error message, deploy artifact, or other
  channel an unprivileged process can read.
- **Command injection** — anywhere user-controlled or forge-controlled
  data flows into a shell invocation, an `exec`, or a `git` argv.
- **Path traversal** — anywhere user-controlled or forge-controlled
  data flows into a filesystem path (the wiki cache, credentials
  file, project config layering, etc.).
- **Privilege escalation** — anywhere a request to gaia-mcp ends up
  acting under credentials it shouldn't have access to (e.g. across
  bearer-token tenants, or with the daemon's host credentials
  instead of the caller's).
- **Prompt injection that escapes the trust marker** — gaia tags
  forge-supplied content (issue bodies, comments, wiki content,
  etc.) with `_trust: external` so an agent can branch on the
  marker. A bug that strips or mis-applies the marker so untrusted
  content reaches the model unmarked counts as a vulnerability.
- **Container escape via the published Dockerfile** — gaia-mcp is
  packaged as an OCI image; a bug in the Dockerfile or its
  entrypoint that lets a hostile call out of the runtime is in
  scope.

In scope but lower priority: denial-of-service against gaia-mcp
(rate-limit bypasses, slowloris, oversize requests), TLS
configuration mistakes in the bundled `deploy/nginx.conf` example,
and similar.

Out of scope:

- Bugs in upstream forges (Forgejo / Gitea / GitHub) — please report
  those upstream. gaia's job is to be a thin protocol-translation
  layer; if Forgejo serves a malicious response, that's a Forgejo
  bug. (gaia bugs that fail to defend against a malicious response
  *are* in scope.)
- Bugs in the underlying Go runtime, OS, or container runtime.
- The operator's own deploy choices (running gaia-mcp on a public
  IP without TLS, baking a token into a public Docker image, etc.)
  — gaia's bind policy refuses public bind without an explicit
  acknowledgement, but social-engineering an operator into passing
  the flag is not a gaia bug.
- Findings that boil down to "the operator gave their token to
  someone untrusted." Token compromise via legitimate operator
  action is the operator's incident.

## Recent fixes (illustrative scope)

These are the kind of issue gaia treats as in-scope, fixed in #147:

- Chain runner shell injection — substituted variables flowed
  unquoted into `sh -c`. Fixed by shell-quoting at substitution
  time. (#135)
- Wiki cache path traversal — owner / repo / slug were joined into
  filesystem paths without validation. Fixed by allowlist check
  before path-join. (#136)
- Token in argv — the wiki cache used to pass the auth header in
  the URL on `git`'s argv, visible in `ps`. Fixed by routing the
  header through `GIT_CONFIG_*` env vars. (#137)
- Indirect prompt injection — forge-supplied text reached the
  agent's context window indistinguishable from operator input.
  Fixed by tagging external content with `_trust: external`
  markers in JSON / `<<<EXTERNAL ... EXTERNAL>>>` delimiters in
  pretty output. (#146)

These are a useful calibration for the threshold: anything in this
class is worth a private report.

## How to report a vulnerability

**Email** the report to **aidev@stewartbrothers.com.au** with the
subject prefix `[gaia security]`. PGP key on request.

Please include:

1. A description of the issue and which threat-model bucket above
   it belongs in (or "doesn't fit, but here's why I think it's
   worth reporting").
2. A repro: ideally a minimal command line, a sample input
   (issue body, chain YAML, MCP request payload, etc.), and the
   observed behavior. If the repro requires a specific gaia
   version or build flag, call that out.
3. Your assessment of severity (so we can sanity-check our own).
4. Whether you have a fix in mind. We're happy to accept a
   patch under the same private channel; we'll cut a public PR
   referencing your report only after the fix lands and the
   embargo lifts.

We do **not** want vulnerability reports filed as public issues
(`gaia issue create` against the canonical Forgejo). If you've
already filed one, email us anyway — we'll triage and convert
to a private discussion.

## Disclosure timeline

- **Acknowledgement** within 7 days of receipt. We'll let you
  know we've seen the report and what we think of severity.
- **Fix in flight** within 30 days for high-severity issues
  (token leakage, RCE, auth bypass), 90 days for the rest.
- **Public disclosure** after the fix has shipped in a tagged
  release and the operator population has had a reasonable
  window to upgrade — typically 14 days post-release, longer
  for bugs that require a coordinated upstream change.
- **CVE assignment** if applicable. gaia is small enough that
  most issues land as a release-notes line item rather than a
  formal CVE, but we'll request one for anything where the
  exploit path is plausible against a default deployment.

If we miss a deadline, ping us. The 90-day cap is the floor —
we'd rather coordinate longer with you than ship a half-fixed
release.

## Scope: what's covered

Vulnerabilities in:

- The `gaia` CLI binary as built from `cmd/gaia/`.
- The `gaia-mcp` server binary as built from `cmd/gaia-mcp/`.
- The Dockerfile in this repo and the OCI images published from
  it.
- The Homebrew formula at `Formula/gaia.rb` (and the build it
  drives).
- The bundled `deploy/` examples — `docker-compose.example.yml`,
  `nginx.conf`. We won't claim "the example is a recommended
  deploy"; we will fix bugs in it that would mislead an operator
  following it directly.
- The shipped chain scenarios under `core/chain/` and
  documentation under `docs/`.

Not covered:

- Forks under different module paths.
- Mirrors not under our control (the `github.com/stewartbrothers/gaia`
  mirror is in scope; arbitrary other mirrors aren't).
- Operator-built images using gaia binaries — we cover the
  binaries' behavior, not the surrounding infrastructure.

## Acknowledgements

No reports received yet. This section will list the reporters and
the issue numbers as fixes ship; if you want anonymity, please say
so in your report and we'll honour it.
