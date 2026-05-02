# Authentication

`gaia` reads credentials from a layered store with four sources, in
descending precedence:

1. `--token` flag (explicit one-off; not recommended for routine use).
2. Environment variables, in fallback order:
   - Forgejo: `FORGEJO_TOKEN` → `GITEA_TOKEN` (the tea-CLI convention)
   - GitHub: `GITHUB_TOKEN` → `GH_TOKEN` (the gh-CLI convention)
   - Or a profile-pinned `token_env` name (see `.gaia/config.yaml`).
3. Project credentials: `.gaia/credentials.yaml` inside the repo root.
4. Global credentials: `~/.config/gaia/credentials.yaml` (or
   `$XDG_CONFIG_HOME/gaia/credentials.yaml`).

For 90% of flows you never set any of these by hand — `gaia auth ...`
writes them.

## Quickstart

```bash
# Forgejo / Gitea
gaia auth forgejo https://git.example.com
# → prompts for a Personal Access Token, validates via /user, stores

# GitHub
gaia auth gh
# → prompts for a fine-grained PAT, validates via api.github.com

# Verify
gaia whoami       # uses the just-stored credential — no env vars needed
gaia auth status  # list everything (token values redacted)
```

## Per-project credentials

```bash
# Inside a checkout where you want a separate identity:
gaia auth forgejo https://git.example.com --project
# → writes to .gaia/credentials.yaml AND adds it to .gitignore
```

Project credentials shadow global credentials on a per-host basis. So
a repo-local `--project` auth for `git.example.com` overrides the
global one only inside that checkout; everywhere else, the global
credential still applies.

To skip the auto-gitignore (rare; only useful if you manage
.gitignore via a generator):

```bash
gaia auth forgejo https://git.example.com --project --no-gitignore
```

## Where the files live

| Purpose | Path |
|---------|------|
| Global config (non-secret) | `~/.config/gaia/config.yaml` |
| Global credentials | `~/.config/gaia/credentials.yaml` |
| Project config (non-secret) | `.gaia/config.yaml` (in repo root, **committable**) |
| Project credentials | `.gaia/credentials.yaml` (in repo root, gitignored — see below) |

Both `XDG_CONFIG_HOME` and `HOME` are honored for the global paths.

`config.yaml` carries non-secret host metadata (provider, api_url,
default_profile, default_repo). `credentials.yaml` carries the token
+ login. They are deliberately separate: a config file may be
committable in some flows; credentials should never be.

### Project config example

A repo's `.gaia/config.yaml` typically looks like:

```yaml
default_profile: stewartbrothers
default_repo: Gerwood/gaia       # short-circuits --repo when set

profiles:
  stewartbrothers:
    provider: forgejo
    api_url: https://your-forge.example.com/api/v1
```

With this committed, every contributor working in the checkout gets
`gaia issue list`, `gaia pr create ...`, etc. without needing
`--provider`, `--api-url`, or `--repo` flags. They still need a
token (from env or `gaia auth ...`), but everything else is
inferred.

The layering order is **project > global > env > flags**; project
config can shadow a contributor's global default for one repo (e.g.,
"in this checkout, use the corporate Forgejo, not my personal
GitHub").

## Permissions

`credentials.yaml` files are written `0600` and the parent `.gaia/`
or `.config/gaia/` directories are created `0700`. `gaia auth` writes
atomically (tempfile + rename) so an interrupted run never leaves a
partial file.

## Gitignore protection

The repo's `.gitignore` ships a structural rule covering project
credentials:

```
.gaia/credentials*
```

The trailing `*` catches the canonical `credentials.yaml` plus
any rotation convention (`credentials.bak`, `credentials_old`,
`credentialsBACKUP`, etc). This means **the credential file is
gitignored by structural rule** — not dependent on the runtime
auth flow having been invoked in a specific way.

`auth.EnsureGitignored` (which fires when `gaia auth forgejo
--project` is run) stays as belt-and-braces for repos cloned
before this rule landed. Both gates are belt-and-braces; either
alone is enough.

Filed history: see #105.

## File format

```yaml
forgejo:
  your-forge.example.com:
    api_url: https://your-forge.example.com/api/v1
    token: <opaque>
    user: Gerwood
github:
  github.com:
    api_url: https://api.github.com
    token: <opaque>
    user: gerwood
```

Top-level keys are provider names; second-level are hostnames. The
token + user fields are always present; `api_url` is included so
`gaia` can run with no other config (`gaia whoami` resolves the URL
from the credential).

## Removing credentials

```bash
gaia auth logout                       # interactive picker if multiple are stored
gaia auth logout forgejo               # exact match (single forgejo credential)
gaia auth logout forgejo:git.example   # exact match (provider:host)
```

## Token redaction

Token values **never** appear in any gaia output — not in
`gaia auth status` (prints `TokenSet: true|false`), not in error
messages (the HTTP client's `scrubError` defensively strips), not in
log lines. The internal `core/auth.Credential.String()` and
`.GoString()` methods are tested across `%s`, `%v`, `%+v`, and `%#v`
verbs to guarantee this.

## Token scopes

For a self-hosted Forgejo, recommended PAT scopes:

- `read:repository`, `write:repository` — for issue/PR/label CRUD
- `write:issue` — for comments + labels
- `read:user` — for `whoami` validation

For GitHub fine-grained PATs:

- Contents: read
- Issues: read + write
- Pull requests: read + write

## Troubleshooting

**`exit code 4 (Auth)` from any command** → token missing or rejected.
Run `gaia auth status` to see what's configured; `gaia auth forgejo
<url>` to re-record.

**`no provider configured`** → no credentials stored AND no
`--provider`/`GAIA_PROVIDER` set. Run `gaia auth forgejo <url>` first.

**Wrong forge picked when multiple credentials are configured** → the
one-credential auto-pick only works when there's exactly one stored.
Use `--provider forgejo --api-url <url>` or set `GAIA_PROFILE` /
`default_profile` in `~/.config/gaia/config.yaml` to disambiguate.
