#!/usr/bin/env bash
# scripts/cut-release.sh — operator one-liner for cutting a release.
#
# Usage:
#
#   ./scripts/cut-release.sh vX.Y.Z
#
# What it does:
#
#   1. Sanity-check the working tree (clean, on main, up-to-date).
#   2. Verify CHANGELOG.md has a section for the new version (the
#      release-prep PR should have moved [Unreleased] entries into a
#      [vX.Y.Z] section already).
#   3. Run the local gate (`make fmt vet lint cover build`).
#   4. Create an annotated tag and push it.
#   5. Tail the operator's eyeballs at the Forgejo Actions UI for
#      the release workflow run.
#
# What it does NOT do:
#
#   - Edit CHANGELOG.md. That happens in a separate release-prep PR
#     so reviewers can see the release notes before the tag flies.
#     See RELEASING.md for the full procedure.
#   - Push to GitHub directly. The release workflow handles that
#     when GITHUB_MIRROR_SSH_KEY is configured (#47).
#   - Bump the version in any source file. gaia's version is
#     injected from the git tag at build time; there's nothing to
#     update by hand.

set -euo pipefail

if [ $# -ne 1 ]; then
  cat >&2 <<EOF
usage: $0 vX.Y.Z

Cuts a release from the current main commit. Run this AFTER the
release-prep PR (which moves [Unreleased] CHANGELOG entries into
the [vX.Y.Z] section) has merged. See RELEASING.md.
EOF
  exit 64
fi

TAG="$1"

# Tag-format guard. Keep the regex broad enough to cover
# pre-releases (-rc.1, -beta.2, +metadata, etc.).
if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?(\+[A-Za-z0-9.]+)?$ ]]; then
  echo "error: tag '$TAG' does not match SemVer pattern vX.Y.Z[-PRERELEASE][+BUILD]" >&2
  exit 64
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

# 1. Working tree must be clean. Releases shipped from a dirty tree
# are reproducibility nightmares.
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "error: working tree is dirty. Commit, stash, or revert first." >&2
  git status --short >&2
  exit 1
fi

# 2. Must be on main.
current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$current_branch" != "main" ]; then
  echo "error: not on main (current: $current_branch). Releases are cut from main." >&2
  exit 1
fi

# 3. Must be up-to-date with origin/main.
git fetch origin main --quiet
local_sha="$(git rev-parse HEAD)"
remote_sha="$(git rev-parse origin/main)"
if [ "$local_sha" != "$remote_sha" ]; then
  echo "error: local main ($local_sha) differs from origin/main ($remote_sha)." >&2
  echo "       run 'git pull --ff-only' first." >&2
  exit 1
fi

# 4. Tag must not already exist (locally or remotely).
if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "error: tag $TAG already exists locally. Delete it first if you really want to re-cut." >&2
  exit 1
fi
if git ls-remote --tags origin "refs/tags/$TAG" | grep -q "$TAG"; then
  echo "error: tag $TAG already exists on origin. Delete it on the remote first if you really want to re-cut." >&2
  exit 1
fi

# 5. CHANGELOG.md must have a section for the new tag (without the
# leading 'v', matching the Keep-A-Changelog convention this repo
# uses). Cheap grep is fine; the format is `## [X.Y.Z]`.
version_no_v="${TAG#v}"
if ! grep -q "^## \[${version_no_v}\]" CHANGELOG.md; then
  cat >&2 <<EOF
error: CHANGELOG.md has no [${version_no_v}] section.

Open a release-prep PR first that moves [Unreleased] entries into
a new [${version_no_v}] section, get it merged, then re-run this
script. See RELEASING.md.
EOF
  exit 1
fi

# 6. Local gate. CI re-runs the same on the tagged commit, but
# catching a failure here saves the operator a 5-minute round-trip.
echo "==> Running local gate (fmt vet lint cover build)…"
if ! make fmt vet lint cover build; then
  echo "error: local gate failed. Fix it before tagging." >&2
  exit 1
fi

# 7. Tag + push.
echo "==> Creating annotated tag $TAG"
git tag -a "$TAG" -m "release: $TAG"

echo "==> Pushing $TAG to origin"
git push origin "$TAG"

cat <<EOF

✓ Tag $TAG pushed.

The release workflow at .forgejo/workflows/release.yml is now
running. Watch it at:

  https://github.com/stewartbrothers/gaia/actions

Expected on success:

  - Forgejo release $TAG with all archives + checksums attached.
  - A 'release: bump Homebrew formula to $TAG' commit on main
    updating Formula/gaia.rb (requires GORELEASER_TAP_DEPLOY_KEY
    secret — see RELEASING.md).
  - $TAG mirrored to github.com/stewartbrothers/gaia (requires
    GITHUB_MIRROR_SSH_KEY secret — see docs/mirroring.md).

If the workflow fails, see the per-step error annotations and the
recovery steps in RELEASING.md.
EOF
