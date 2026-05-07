#!/usr/bin/env bash
# scripts/mirror-to-github.sh — push the canonical Forgejo repo to the
# public GitHub mirror at github.com/stewartbrothers/gaia.
#
# Usage:
#
#   ./scripts/mirror-to-github.sh
#
# The script is **idempotent**: re-running converges on the canonical
# state. It refuses to overwrite history (no --force); if `main` has
# diverged on the mirror, fix the divergence by hand after confirming
# with the team.
#
# What it pushes:
#
#   - `main` → `github/main`
#   - every local `v*` tag → `github/<tag>`
#
# What it does NOT push:
#
#   - feature branches (mirror is for the public default branch only)
#   - non-`v*` tags (snapshot/scratch tags stay on the canonical repo)
#
# Prerequisites:
#
#   git remote add github git@github.com:stewartbrothers/gaia.git
#   # plus an SSH key in your agent that GitHub recognizes for the
#   # repo (deploy key with write access, or a personal SSH key on a
#   # collaborator account).
#
# This script is also invoked by the .forgejo/workflows/mirror.yml
# workflow on every push to `main` (when the GITHUB_MIRROR_SSH_KEY
# secret is configured). See docs/mirroring.md for setup details.

set -euo pipefail

REMOTE="${REMOTE:-github}"

if ! git remote get-url "$REMOTE" >/dev/null 2>&1; then
  cat >&2 <<EOF
error: git remote '$REMOTE' is not configured.

Add it with:

  git remote add $REMOTE git@github.com:stewartbrothers/gaia.git

then re-run this script. See docs/mirroring.md for the full setup.
EOF
  exit 1
fi

echo "==> Pushing main to $REMOTE/main"
git push "$REMOTE" main

# Push all v* tags. Use the explicit refspec rather than --tags so that
# scratch tags (e.g. test-build-2024) on the canonical repo don't leak
# to the public mirror.
echo "==> Pushing v* tags to $REMOTE"
# `git push` with a wildcard refspec is the same operation a
# `git push --tags` would do, restricted to v*-prefixed tags. The
# `--no-verify` flag is intentionally omitted — pre-push hooks should
# fire for the mirror push too.
git push "$REMOTE" 'refs/tags/v*:refs/tags/v*'

echo "==> Mirror push complete."
