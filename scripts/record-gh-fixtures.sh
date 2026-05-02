#!/usr/bin/env bash
# Records api.github.com responses into core/github/testdata/fixtures/
# so the github provider's recorded-fixture tests run against real
# wire-shapes (catches drift between what we hand-write and what
# GitHub actually returns).
#
# Usage:
#   ./scripts/record-gh-fixtures.sh             # uses cli/cli (public, no auth needed)
#   GH_TOKEN=ghp_... ./scripts/record-gh-fixtures.sh
#
# Re-run when:
#   * a new method-under-test wants a recorded fixture
#   * a github API version bump might change a wire shape
#   * a test fails because the captured fixture is older than the
#     `X-GitHub-Api-Version: 2022-11-28` server contract
#
# The captured files are committed to the repo so CI runs against the
# same wire shapes regardless of network availability.

set -euo pipefail

REPO_OWNER="${REPO_OWNER:-cli}"
REPO_NAME="${REPO_NAME:-cli}"
FIXTURE_DIR="$(git rev-parse --show-toplevel)/core/github/testdata/fixtures"

mkdir -p "$FIXTURE_DIR"

fetch() {
  local out_file="$1"
  local path="$2"
  echo "→ $out_file ← $path" >&2
  local args=(
    -sSf
    -H 'Accept: application/vnd.github+json'
    -H 'X-GitHub-Api-Version: 2022-11-28'
  )
  if [[ -n "${GH_TOKEN:-}" ]]; then
    args+=(-H "Authorization: Bearer ${GH_TOKEN}")
  fi
  curl "${args[@]}" "https://api.github.com${path}" > "$FIXTURE_DIR/$out_file"
}

base="/repos/${REPO_OWNER}/${REPO_NAME}"

fetch "cli-cli-issues-list.json"     "${base}/issues?state=open&per_page=5"
fetch "cli-cli-issue-1.json"         "${base}/issues/1"
fetch "cli-cli-pulls-list.json"      "${base}/pulls?state=open&per_page=3"
fetch "cli-cli-pull-1.json"          "${base}/pulls/1"
fetch "cli-cli-releases-list.json"   "${base}/releases?per_page=3"
fetch "cli-cli-release-tag.json"     "${base}/releases/tags/v2.79.0"
fetch "cli-cli-comments-issue.json"  "${base}/issues/1/comments?per_page=10"
fetch "cli-cli-search-issues.json"   "/search/issues?q=repo:${REPO_OWNER}/${REPO_NAME}+is:issue+label:bug&per_page=3"

echo "✓ Recorded $(ls "$FIXTURE_DIR" | wc -l | tr -d ' ') fixtures into $FIXTURE_DIR" >&2
