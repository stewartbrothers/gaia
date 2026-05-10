# Configuration

`gaia` reads configuration from four layered sources. Higher layers
override lower ones on a field-by-field basis:

| Layer | Source | Purpose |
|------:|--------|---------|
| 4 (highest) | CLI flags (`--profile`, `--provider`, `--api-url`, `--repo`) | One-off overrides for a single invocation. |
| 3 | Env vars (`GAIA_PROFILE`, `GAIA_PROVIDER`, `FORGEJO_API_URL`, plus token vars) | Per-shell overrides. |
| 2 | **Project config** — `.gaia/config.yaml` in the repo root | Per-project pins (recommended for `default_profile`, `default_repo`). |
| 1 (lowest) | **Global config** — `$XDG_CONFIG_HOME/gaia/config.yaml` (or `$HOME/.config/gaia/config.yaml`) | Profile **definitions** shared across every project. |

Tokens never live in either YAML file. They come from env vars or
the per-host credentials store (`credentials.yaml`); see
[`docs/auth.md`](auth.md).

A missing config file is fine — env-vars-only is fully supported.

## Project config (recommended) — `.gaia/config.yaml`

The project-local file is where `default_profile` and `default_repo`
**should** be pinned. Committed to the repo, it lets every
contributor in the checkout run `gaia` bare:

```yaml
# .gaia/config.yaml — committed, no secrets
default_profile: corp-forge
default_repo: myorg/myrepo

profiles:
  corp-forge:
    provider: forgejo
    api_url: https://git.corp.example.com/api/v1
```

With this in place:

```bash
gaia issue list                 # works — no flags, no env vars
gaia pr create --title ... \
  --head feature/x --base main  # works — no --provider, --api-url, --repo
gaia whoami                     # works
```

The file is **non-secret and committable** by design (no tokens,
just provider + URL + repo defaults). Some teams prefer to gitignore
it and have each contributor write their own — both flows work.

If the project also needs a credentials file separate from the user's
global one, it goes at `.gaia/credentials.yaml` (gitignored — see
`docs/auth.md`).

## Global config — `~/.config/gaia/config.yaml`

The global file is shared across every checkout on the system. It
should carry **profile definitions only** — provider + api_url
(+ optional `token_env`) for each forge the user works with.

```yaml
# ~/.config/gaia/config.yaml — profile DEFINITIONS only
profiles:
  corp-forge:
    provider: forgejo
    api_url: https://git.corp.example.com/api/v1
    token_env: CORP_FORGEJO_TOKEN

  github:
    provider: github
    api_url: https://api.github.com
```

Notice what is **not** in this file: `default_profile`,
`default_repo`. See the next section for why.

## Multi-project safety

> **Treat the global config as a multi-tenant surface.** Anything you
> write there applies to every project on the same machine.

The contamination footgun: setting `default_profile: corp-forge`
globally means every `gaia` invocation from anywhere on the system
inherits that default. A call from inside a GitHub-hosted project
checkout (without a project `.gaia/config.yaml` to override) will
quietly resolve as `corp-forge` — wrong forge, wrong api_url,
possibly wrong credentials. Best case: an unhelpful 401. Worst case:
the call appears to succeed against the wrong forge because the repo
slug happens to exist there too.

The same applies to `default_repo` — globally pinning it makes every
naked `gaia issue list` from any directory resolve to that one repo.

### Checklist

- **Global config carries profile definitions only.** No
  `default_profile`, no `default_repo` at the top level.
- **Per-project pinning lives in `.gaia/config.yaml`.** Every
  checkout that wants bare-`gaia` ergonomics gets its own file with
  `default_profile` + `default_repo`.
- **Tokens stay out of either YAML.** Use env vars (per-shell
  scoping) or the per-host `credentials.yaml` written by `gaia auth`
  (host-keyed, not profile-keyed).
- **One-off cross-forge calls use `--profile <name>`** (or
  `GAIA_PROFILE=<name> gaia ...`) rather than mutating global state.
- **Audit periodically.** `cat ~/.config/gaia/config.yaml` should
  show only `profiles:` at the top level. If you see
  `default_profile` or `default_repo` there, move them to the
  relevant project's `.gaia/config.yaml` and delete from the global.

### What does the field reference recommend?

The field-by-field guidance:

| Field | Where | Notes |
|-------|-------|-------|
| `profiles:` | global (or project) | Forge definitions. Same name in both layers → project shadows global. |
| `default_profile` | **project only (recommended)** | Setting it globally contaminates every other project. |
| `default_repo` | **project only (recommended)** | Globally meaningless — `default_repo` is a per-checkout concept. |
| `cache:` | either | TTLs, max size. Project replaces global wholesale when set (see `docs/cache.md`). |

## Field reference

```yaml
default_profile: <name>          # project-only-recommended
default_repo: <owner>/<name>     # project-only-recommended

profiles:
  <name>:
    provider: forgejo|github     # required
    api_url: https://.../api/v1  # required
    token_env: VAR_NAME          # optional override (else canonical env)

cache:                           # optional; see docs/cache.md
  enabled: true
  ttl_seconds:
    single: 300
    list: 30
  max_size_mb: 100
```

| Field             | Type   | Required | Notes                                          |
|-------------------|--------|----------|------------------------------------------------|
| `default_profile` | string | no       | Picked when neither flag nor env names a profile. **Pin per-project, not globally.** |
| `default_repo`    | string | no       | Used when neither `--repo` nor git-remote autodetect supplies a target. **Pin per-project, not globally.** |
| `profiles`        | map    | no       | Profile name → profile. Project keys shadow global keys with the same name. |
| `profiles[].provider` | string | yes  | `forgejo` or `github`.                         |
| `profiles[].api_url`  | string | yes  | The forge's `/api/v1` (or equivalent) base.    |
| `profiles[].token_env` | string | no | Env var to read the token from. Falls back to canonical (`FORGEJO_TOKEN`/`GITHUB_TOKEN`) when unset or empty. |

**Tokens never go in the YAML file.** The file may be world-readable
in some setups (dotfile repos, shared homes); env-var-only token
sourcing keeps tokens out of those exposures.

## Repo resolution order

For repo-scoped commands (`issue list`, `pr create`, etc.), gaia
resolves the target `owner/name` in this order (first match wins):

1. **`--repo owner/name`** flag — explicit override.
2. **Git-remote autodetect** — parses `origin`'s URL from the cwd
   (see `core/autodetect/parse.go`). Works in any normal git
   checkout with no extra config.
3. **Project `default_repo`** — load-bearing for forges where the
   SSH push host and the HTTPS API host differ (e.g.,
   `repo.example.com` SSH but `git.example.com` API): autodetect
   parses the SSH host but the credential store is keyed by API
   host, so step 2 can't resolve a credential. Pinning
   `default_repo` short-circuits the autodetect fallback.
4. Otherwise: error — `pass --repo owner/name`.

## Environment variables

| Var                | Purpose                                                  |
|--------------------|----------------------------------------------------------|
| `GAIA_PROFILE`     | Selects which profile to use; overrides `default_profile`.|
| `GAIA_PROVIDER`    | Overrides the profile's provider field.                  |
| `FORGEJO_API_URL`  | Overrides the profile's api_url for forgejo.             |
| `FORGEJO_TOKEN`    | Canonical Forgejo token. Used when the profile has no `token_env`, or when the named `token_env` is empty. |
| `GITEA_TOKEN`      | Forgejo fallback (community-standard name; same use as `FORGEJO_TOKEN`). |
| `GITHUB_TOKEN`     | Canonical GitHub token. Same rule.                       |
| `GH_TOKEN`         | GitHub fallback (matches `gh` CLI's convention).         |
| `XDG_CONFIG_HOME`  | Honored for the global config-file location.             |

## Token resolution order

Per-invocation:

1. If the chosen profile has `token_env` set and that env var is
   non-empty, use it.
2. Otherwise, read the canonical env var for the resolved provider:
   `FORGEJO_TOKEN` (then `GITEA_TOKEN`) for `forgejo`,
   `GITHUB_TOKEN` (then `GH_TOKEN`) for `github`.
3. If still empty, the token stays empty; operations that require
   auth fail with exit code 4 (`Auth`).

The per-host `credentials.yaml` (written by `gaia auth ...`) is
loaded separately by the auth layer; see [`docs/auth.md`](auth.md)
for the full credentials-store flow.

## Logging and redaction

The internal `Resolved` value's `String()` method redacts the token
(prints `TokenSet:true|false` instead of the value). Don't bypass it
— never `fmt.Printf("%+v", resolved)` directly.

## Examples

### Per-project pin, bare `gaia` calls

`.gaia/config.yaml` in the repo root:

```yaml
default_profile: corp-forge
default_repo: myorg/myrepo
profiles:
  corp-forge:
    provider: forgejo
    api_url: https://git.corp.example.com/api/v1
```

`~/.config/gaia/config.yaml` (global, unchanged across projects):

```yaml
profiles:
  corp-forge:
    provider: forgejo
    api_url: https://git.corp.example.com/api/v1
  github:
    provider: github
    api_url: https://api.github.com
```

```bash
export FORGEJO_TOKEN=glat_xxx     # token in env, never in YAML
cd ~/projects/myrepo
gaia issue list                   # → corp-forge / myorg/myrepo
gaia pr view 42                   # → corp-forge / myorg/myrepo
```

### Multi-forge user, two projects

User has two projects: `~/work/corp-app` (corp Forgejo) and
`~/personal/side-project` (GitHub). Each gets its own
`.gaia/config.yaml`:

```yaml
# ~/work/corp-app/.gaia/config.yaml
default_profile: corp-forge
default_repo: corp-org/corp-app
profiles:
  corp-forge:
    provider: forgejo
    api_url: https://git.corp.example.com/api/v1
```

```yaml
# ~/personal/side-project/.gaia/config.yaml
default_profile: github
default_repo: alice/side-project
```

The global config carries shared profile definitions and **no**
defaults:

```yaml
# ~/.config/gaia/config.yaml
profiles:
  corp-forge:
    provider: forgejo
    api_url: https://git.corp.example.com/api/v1
  github:
    provider: github
    api_url: https://api.github.com
```

`cd ~/work/corp-app && gaia issue list` and
`cd ~/personal/side-project && gaia issue list` each resolve to the
right forge with no flags. Switching directories switches forges.

### Anti-pattern (do not do this)

```yaml
# ~/.config/gaia/config.yaml — DO NOT pin defaults here
default_profile: corp-forge      # ← contaminates every other project
default_repo: corp-org/corp-app  # ← globally meaningless
profiles:
  corp-forge: { ... }
```

With this in place, running `gaia issue list` from
`~/personal/side-project` will resolve as `corp-forge` against the
corp Forgejo — likely failing with a 401, possibly succeeding
against an unrelated repo. Move `default_profile` and `default_repo`
into each project's `.gaia/config.yaml` instead.

### One-shot, no config file

```bash
export FORGEJO_TOKEN=glat_xxx
gaia --provider forgejo --api-url https://git.example/api/v1 \
  --repo owner/name pr list
```

### One-off cross-forge call

Inside a Forgejo-pinned project, but you want one call against
GitHub:

```bash
gaia --profile github --repo octocat/hello-world pr view 42
```

Don't edit the global config to switch forges — pass `--profile`
(or `GAIA_PROFILE=github`) for the single call.

## Recommended `.gitignore` entries

Every project that uses `gaia` should keep a small set of paths out
of version control. The exact block is shipped as `gaia gitignore`
so there is one source of truth — the docs you are reading and the
CLI command read the same `//go:embed`'d
`internal/gitignore/recommended.txt`.

```
# gaia credentials store (auto-installed by 'gaia auth')
.gaia/credentials*

# gaia insights DB (Phase 9, defaults to XDG state — catch in-tree overrides)
.gaia/insights.db
.gaia/insights.db-wal
.gaia/insights.db-shm
.gaia/insights/
```

| Entry | Why |
|---|---|
| `.gaia/credentials*` | `gaia auth ...` writes the credential file here. The auth command auto-installs this entry on first use, but the line is documented explicitly so a project that hand-rolls the credentials file (or copies it from another checkout) doesn't accidentally commit a token. |
| `.gaia/insights.db`, `.gaia/insights.db-wal`, `.gaia/insights.db-shm` | Phase 9 (`gaia insights`) lands a SQLite-backed local-usage store. Defaults to XDG state, but operators who override the location into the working tree need every SQLite glob sibling gitignored. The `-wal` / `-shm` files are write-ahead log + shared memory; without them gitignored, they slip into commits. |
| `.gaia/insights/` | Reserved directory in case a future override wants to scope insights state under a directory rather than a single DB file. |

`.gaia/config.yaml` and `.gaia/chains/*.yaml` are deliberately **not**
in the list — those are committable project artefacts (non-secret
defaults and chain definitions). Adding them to the recommended
block would push correct project state out of version control.

### Append once

```bash
gaia gitignore >> .gitignore
```

### Audit existing projects

```bash
gaia gitignore --check
```

Exit code `0` if every recommended entry is present; non-zero
otherwise, with the list of missing entries printed to stdout. Pair
with `--quiet` for CI gating that wants the exit code only:

```bash
gaia gitignore --check --quiet || {
  echo ".gitignore is out of date; run 'gaia gitignore >> .gitignore'"
  exit 1
}
```

The same content is exposed to MCP clients as a static resource at
`gaia://gitignore` (MIME type `text/plain`). Agents driving
`gaia-mcp` can `resources/read` the URI to pull the recommended
block without shelling out.

## See also

- [`docs/auth.md`](auth.md) — credentials store, token sourcing,
  gitignore rules.
- [`docs/cache.md`](cache.md) — `cache:` block in detail.
- `core/config/config.go` — the `Merge` function that folds project
  over global.
- `internal/cli/repo.go` — the repo resolution chain.
