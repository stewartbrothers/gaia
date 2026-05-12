# Dogfood baseline: `gaia config doctor` (#277)

## Why this file has no bytes-saved numbers

`gaia config doctor` is **not** a forge-read command. There is no
upstream API call to compare against — the data doctor inspects is
entirely local (the operator's `~/.config/gaia/config.yaml`,
`~/.config/gaia/credentials.yaml`, the project's `.gaia/config.yaml`
and `.gaia/credentials.yaml`, plus relevant env vars).

The dogfood-comparison contract (`scripts/dogfood-compare.sh`)
benchmarks gaia commands against the equivalent raw forge API
response so we can show field-projection wins (e.g. `~22 KB` raw
vs `~3 KB` projected for `gaia issue list`). That contract does
not apply here.

## What doctor replaces

A pre-doctor world for "is my gaia setup contaminating other
projects?" looked like:

```bash
# Look for the smoking gun by hand.
cat ~/.config/gaia/config.yaml
ls -l ~/.config/gaia/credentials.yaml
grep -r 'default_profile\|default_repo' ~/.config/gaia/
# …repeat for project .gaia/, repeat the mode check, etc.
```

Doctor folds those checks into one command with a stable code per
finding, a structured JSON envelope for CI gating, and a one-line
remediation per smell. The win is **operator clarity**, not bytes.

## CLI shape

```bash
gaia config doctor                # human-readable: one finding/line + summary
gaia config doctor --format json  # standard envelope, findings as data records
gaia config doctor --strict       # promote WARN to ERR for CI gating
gaia config doctor --quiet        # exit-code only, suitable for CI scripts
```

## Exit-code contract

- `0` — no `ERR` findings (and no `WARN` if `--strict` is set).
- `1` — at least one `ERR` finding.

## Finding inventory

Each finding carries a stable `code` (greppable from JSON):

| Code | Level | Trigger |
|---|---|---|
| `global-default-profile` | WARN | global config sets `default_profile` |
| `global-default-repo` | WARN | global config sets `default_repo` |
| `credentials-file-mode` | ERR | global credentials file mode > `0600` |
| `project-credentials-not-gitignored` | ERR | project `.gaia/credentials.yaml` exists but `.gitignore` doesn't cover it |
| `env-and-credentials-overlap` | WARN | both a token env var and a stored credential for the same provider |
| `profile-no-provider` | ERR | resolved profile has no `provider` |
| `profile-no-api-url` | ERR | resolved profile has no `api_url` |
| `token-env-empty` | WARN | `token_env` names an empty var and no canonical fallback is set |
| `default-profile-missing` | WARN | `default_profile` names a missing profile |
| `repo-resolution` | INFO | how `owner/name` would resolve in the cwd |
| `config-layers` | INFO | which config layers contributed to the resolved state |

## Out of scope (v1)

- Auto-fix. Doctor reports; the operator runs the suggested
  remediation. Auto-rewriting global config files crosses too
  many trust boundaries.
- Cross-project scanning. Cwd-scoped only.
- MCP exposure. Lands once the CLI shape stabilizes.
