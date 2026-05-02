# Configuration

`gaia` reads configuration from three layered sources, highest precedence
first:

1. **CLI flags** — `--profile`, `--provider`, `--api-url`.
2. **Environment variables** — `GAIA_PROFILE`, `GAIA_PROVIDER`,
   `FORGEJO_API_URL`, plus the token env vars below.
3. **Config file** — `$XDG_CONFIG_HOME/gaia/config.yaml`, falling back
   to `$HOME/.config/gaia/config.yaml`.

A missing config file is fine — the env-vars-only path is fully
supported.

## Config file

```yaml
# ~/.config/gaia/config.yaml
default_profile: stewartbrothers

profiles:
  stewartbrothers:
    provider: forgejo
    api_url: https://your-forge.example.com/api/v1
    token_env: GIT_FORGE_GITEA_TOKEN

  github:
    provider: github
    api_url: https://api.github.com
```

| Field             | Type   | Required | Notes                                          |
|-------------------|--------|----------|------------------------------------------------|
| `default_profile` | string | no       | Picked when neither flag nor env names a profile.|
| `profiles`        | map    | no       | Profile name → profile.                        |
| `profiles[].provider` | string | yes  | `forgejo` or `github`.                         |
| `profiles[].api_url`  | string | yes  | The forge's `/api/v1` (or equivalent) base.    |
| `profiles[].token_env` | string | no | Env var to read the token from. Falls back to canonical (`FORGEJO_TOKEN`/`GITHUB_TOKEN`) when unset or empty. |

**Tokens never go in the YAML file.** The file may be world-readable in
some setups (dotfile repos, shared homes); env-var-only token sourcing
keeps tokens out of those exposures.

## Environment variables

| Var                | Purpose                                                  |
|--------------------|----------------------------------------------------------|
| `GAIA_PROFILE`     | Selects which profile to use; overrides `default_profile`.|
| `GAIA_PROVIDER`    | Overrides the profile's provider field.                  |
| `FORGEJO_API_URL`  | Overrides the profile's api_url for forgejo.             |
| `FORGEJO_TOKEN`    | Canonical Forgejo token. Used when the profile has no `token_env`, or when the named `token_env` is empty. |
| `GITHUB_TOKEN`     | Canonical GitHub token. Same rule.                       |
| `XDG_CONFIG_HOME`  | Honored for the config-file location.                    |

## Token resolution order

For each invocation:

1. If the chosen profile has `token_env` set and that env var is
   non-empty, use it.
2. Otherwise, read the canonical env var for the resolved provider:
   `FORGEJO_TOKEN` for `forgejo`, `GITHUB_TOKEN` for `github`.
3. If still empty, the token stays empty; operations that require auth
   will fail with exit code 4 (`Auth`).

## Logging and redaction

The internal `Resolved` value's `String()` method redacts the token
(prints `TokenSet:true|false` instead of the value). Don't bypass it —
never `fmt.Printf("%+v", resolved)` directly.

## Examples

### One-shot, no config file

```bash
export FORGEJO_TOKEN=glat_xxx
gaia --provider forgejo --api-url https://git.example/api/v1 pr list
```

### Profile from config, override token env

```bash
export GIT_FORGE_GITEA_TOKEN=glat_xxx
gaia pr list                       # uses default_profile
gaia --profile github pr list      # explicit override
```

### Switching forges with one flag

```bash
gaia --profile github pr view 42
gaia --profile stewartbrothers pr view 42
```
