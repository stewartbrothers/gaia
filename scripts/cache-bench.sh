#!/usr/bin/env bash
# cache-bench.sh — measure latency + bytes for a 100×issue-view loop
# with the cache enabled vs. disabled.
#
# Usage:
#   make cache-bench              # offline simulation against an in-process server
#   CACHE_BENCH_LIVE=1 make cache-bench   # against the configured forge
#
# The offline path runs an httptest.Server inside the bench Go binary
# so the script works in CI without forge access.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ "${CACHE_BENCH_LIVE:-0}" == "1" ]]; then
  echo "→ live mode: hitting the configured forge"
  echo "  (set GAIA_REPO=owner/repo + ISSUE_NUMBER to override)"
  exec go run ./scripts/cmd/cache-bench -live
fi

echo "→ offline mode: in-process httptest server simulates a forge"
exec go run ./scripts/cmd/cache-bench
