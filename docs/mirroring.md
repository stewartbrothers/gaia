# Mirroring gaia to GitHub

`gaia` is hosted canonically on a self-hosted Forgejo instance at
`your-forge.example.com/stewartbrothers/gaia`. To make the project
discoverable to a wider audience and unblock contributions from people
without an account on the forge, we maintain a public **read-only mirror**
on GitHub at `github.com/stewartbrothers/gaia`.

This document is the operator runbook. It covers the one-time setup, the
push-to-mirror workflow (manual + automated), and how the mirror
interacts with the release pipeline (#50).

## TL;DR — one-time setup

1. **Create the GitHub repo** (web UI or `gh repo create`):

   ```bash
   gh auth login   # if not already
   gh repo create stewartbrothers/gaia \
     --public \
     --description "Token-trimmed CLI + MCP for Forgejo and GitHub" \
     --homepage https://github.com/stewartbrothers/gaia
   ```

   If `Gerwood` is already taken on github.com, use a different owner
   and update every `stewartbrothers/gaia` reference in `README.md`,
   `docs/install.md`, this file, and `.forgejo/workflows/mirror.yml` to
   match (filed as a follow-up issue if it comes to that).

2. **Add the mirror as a remote on your local checkout** of the
   canonical repo:

   ```bash
   cd ~/projects/forgejo-ai-adaption
   git remote add github git@github.com:stewartbrothers/gaia.git
   ```

3. **Initial mirror push** (one shot):

   ```bash
   ./scripts/mirror-to-github.sh
   ```

   Pushes `main` plus every `v*` tag to GitHub. Subsequent updates can
   be either manual (re-run the script) or automated (configure the
   `mirror.yml` workflow secret — see below).

That's the entire one-time setup. The rest of this doc covers the
ongoing operations.

## Mirror is read-only

GitHub is **mirror-only**. The contract:

- The Forgejo instance is the canonical source of truth. Every commit,
  every issue, every PR, every release lives there first.
- GitHub gets a periodic `git push --mirror`-style update of `main` and
  every `v*` tag. Releases attached to Forgejo tags are eventually
  reflected on GitHub via the release workflow (#50), but the Forgejo
  release is authoritative.
- Issues and PRs opened against the GitHub mirror should be redirected
  to the Forgejo instance. (We may set up an `ISSUE_TEMPLATE` on the
  GitHub side in a future change to surface this directly to drive-by
  contributors; not blocking.)

## Manual mirror push

Use the helper script for a one-shot push at any time:

```bash
./scripts/mirror-to-github.sh
```

What it does:

- Verifies the `github` remote exists (errors with a fix-me hint if
  not).
- Pushes `main` to `github/main`.
- Pushes every local `v*` tag to GitHub (`git push github 'refs/tags/v*'`).

Re-running is safe — pushes converge on the Forgejo state. If `main` on
the mirror has somehow diverged (it shouldn't), the script will fail
with a non-fast-forward error rather than overwrite history. If you've
intentionally rewritten history on Forgejo and need the mirror to catch
up, force-push manually after confirming with the team:

```bash
git push --force-with-lease github main
```

## Automated mirror via Forgejo Actions

The `.forgejo/workflows/mirror.yml` workflow runs on every push to
`main` and pushes the new commits to GitHub automatically. It requires
**one secret** to be configured on the Forgejo repo: an SSH deploy key
with write access to the GitHub mirror.

### Secret setup

1. Generate a dedicated SSH key for the mirror (no passphrase — it's
   for CI):

   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/gaia-mirror -N "" \
     -C "gaia-mirror@stewartbrothers"
   ```

2. Add the **public** key (`gaia-mirror.pub`) to the GitHub mirror as
   a deploy key with **write** access:

   - GitHub → `stewartbrothers/gaia` → Settings → Deploy keys → Add deploy key
   - Title: `forgejo-mirror`
   - Key: paste the contents of `~/.ssh/gaia-mirror.pub`
   - Tick "Allow write access"

3. Add the **private** key as a Forgejo secret on the canonical repo:

   - Forgejo → `stewartbrothers/gaia` → Settings → Secrets → Add secret
   - Name: `GITHUB_MIRROR_SSH_KEY`
   - Value: paste the contents of `~/.ssh/gaia-mirror` (full PEM)

The workflow gates the push step on the secret being non-empty, so if
you don't configure the secret, the workflow runs to a green no-op.
This keeps the workflow file safe to merge before the secret is in
place — useful when the GitHub repo doesn't exist yet.

### What the workflow does

On every push to `main`:

1. Checks out the commit with `fetch-depth: 0` so all tags are
   available locally.
2. If `GITHUB_MIRROR_SSH_KEY` is configured, writes it to
   `~/.ssh/id_ed25519`, adds `github.com` to known hosts, and runs the
   same `scripts/mirror-to-github.sh` the operator runs by hand.
3. If the secret is empty, logs a one-line "mirror skipped — secret
   not configured" message and exits 0.

Failures fail the workflow run; the next successful push to `main`
will retry. Because pushes are idempotent (Git only ships missing
objects), a failed run does not leave the mirror in a broken state.

## Tag mirroring + releases (#50 interaction)

The mirror workflow above pushes `v*` tags as part of every `main`
push, so tags created on Forgejo show up on GitHub on the next
`main` push. For users who want the **release artifacts** mirrored
too, the `release.yml` workflow (#50) gates an additional GitHub
release-creation step on the same SSH-key secret being configured —
when present, the release workflow:

1. Pushes the just-tagged ref to GitHub (covers the case where the
   tag is pushed before any subsequent `main` push).
2. Optionally creates a matching GitHub release with the same
   artifacts attached (TBD — depends on whether we want to maintain
   release notes in two places).

Until the release-side wiring lands, **GitHub releases are not
auto-populated**. The Forgejo release is the canonical artifact host;
the GitHub mirror is for code discovery only. The Homebrew tap (#49)
sources artifacts directly from Forgejo, so this is not a blocker for
the install flow.

## Fallback: GitHub-side scheduled mirror

If the Forgejo runner can't reach `github.com` (firewall, etc.), an
alternative is to run the mirror from GitHub Actions on a schedule:

```yaml
# .github/workflows/mirror-from-forgejo.yml (NOT shipped today; pattern only)
on:
  schedule:
    - cron: '*/15 * * * *'
  workflow_dispatch:
jobs:
  mirror:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: |
          git remote add forgejo https://your-forge.example.com/stewartbrothers/gaia.git
          git fetch forgejo --tags
          git push origin 'refs/remotes/forgejo/main:refs/heads/main' \
                          'refs/tags/v*:refs/tags/v*'
```

We don't ship this today — the Forgejo-side push is sufficient. Filed
as a fallback option only.

## Verification

After the first manual or automated mirror push:

```bash
# Should match the latest commit on the canonical repo:
gh api repos/stewartbrothers/gaia/commits/main --jq '.sha[:7]'
git -C ~/projects/forgejo-ai-adaption rev-parse --short main

# Should list every released tag:
gh api repos/stewartbrothers/gaia/tags --jq '.[].name'
git -C ~/projects/forgejo-ai-adaption tag --list 'v*'
```

If the SHAs match and every tag is present, the mirror is healthy.

## Troubleshooting

- **`Permission denied (publickey)` on push**: the deploy key was
  generated without write access, or the wrong key is configured on
  Forgejo. Re-check the GitHub deploy-key page — "Allow write access"
  must be ticked.
- **`! [rejected] main -> main (non-fast-forward)`**: somebody pushed
  to the mirror directly (the mirror is supposed to be read-only; this
  shouldn't happen). Investigate, then force-push from the canonical
  side after confirming with the team.
- **Workflow logs show "mirror skipped — secret not configured" but
  you set the secret**: the secret name must be exactly
  `GITHUB_MIRROR_SSH_KEY`. Check Forgejo → Settings → Secrets and
  verify the name (Forgejo secret names are case-sensitive).
- **Mirror is stale even though pushes succeeded**: the workflow runs
  on `push: branches: [main]` only. Pushes to feature branches don't
  trigger it. If you need to mirror a non-main branch (rare), use the
  manual script: `git push github <branch>`.
