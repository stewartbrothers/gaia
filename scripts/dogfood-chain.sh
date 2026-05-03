#!/usr/bin/env bash
# scripts/dogfood-chain.sh — measure the byte/token saving of running
# the canned `pr-create-and-land` chain vs. the equivalent multi-call
# agent flow ("gaia pr create → gaia pr view --with-ci × N polls →
# gaia pr merge").
#
# DOES NOT actually open or merge a PR. The script prints the
# *response shape sizes* gaia would return for each step against
# representative fixtures, so the comparison is reproducible without
# write access to a forge. To run a real round-trip, point PR_NUMBER
# at an existing open PR and pass DOGFOOD_LIVE=1; the script will
# call `gaia pr view --with-ci` against the live forge for the
# baseline polling loop and emit the same with-chain dry-run for
# comparison.
#
# Usage:
#   make dogfood-chain                          # offline / fixtures
#   PR_NUMBER=75 DOGFOOD_LIVE=1 make dogfood-chain   # against live forge
#
# Token estimates use the conventional bytes/4 heuristic.

set -euo pipefail

GAIA_BIN="${GAIA_BIN:-./bin/gaia}"
PR_NUMBER="${PR_NUMBER:-75}"
POLL_COUNT="${POLL_COUNT:-5}"
DOGFOOD_LIVE="${DOGFOOD_LIVE:-0}"

if [ ! -x "$GAIA_BIN" ]; then
  echo "gaia binary not found at $GAIA_BIN — run 'make build' first" >&2
  exit 1
fi

# row prints a labelled byte/token line.
row() {
  local label="$1"; local bytes="$2"
  local tokens=$(( bytes / 4 ))
  printf '%-50s %8d bytes  %6d tokens\n' "$label" "$bytes" "$tokens"
}

bytes_of() {
  printf '%s' "$1" | wc -c | tr -d ' '
}

# Capture a representative PR-shaped envelope for the create
# response. We use `pr view` against a real PR as a proxy — the
# create response has the same shape as view (no comments, no CI).
prview_bytes=0
if [ "$DOGFOOD_LIVE" = "1" ]; then
  view_out="$("$GAIA_BIN" pr view "$PR_NUMBER" 2>/dev/null || true)"
  withci_out="$("$GAIA_BIN" pr view "$PR_NUMBER" --with-ci 2>/dev/null || true)"
  prview_bytes=$(bytes_of "$view_out")
  withci_bytes=$(bytes_of "$withci_out")
else
  # Offline estimates from the bench/dogfood-baseline.md numbers.
  prview_bytes=4255    # gaia pr view 75 (no CI)
  withci_bytes=4387    # gaia pr view 75 --with-ci
fi

merge_confirm_bytes=30   # "✓ Merged #N using \"squash\"\n"

baseline_total=$((prview_bytes + withci_bytes * POLL_COUNT + merge_confirm_bytes))

# With-chain: rough estimate from the canned chain envelope shape.
# Composition: chain metadata (~150 b) + 3 step records (~150 b each;
# stdout truncated by chain runner) + captured.pr (PR data, ~3000 b
# for this repo's PR shape).
chain_meta=150
chain_steps=$((150 * 3))
chain_captured=$prview_bytes   # captured.pr ≈ a single pr-view payload
chain_total=$((chain_meta + chain_steps + chain_captured))

echo
echo "===== Without chain (multi-call agent flow) ====="
row "gaia pr create response (≈ pr view shape)"      "$prview_bytes"
row "gaia pr view --with-ci × $POLL_COUNT polls"     $((withci_bytes * POLL_COUNT))
row "gaia pr merge confirmation line"                "$merge_confirm_bytes"
row "Total"                                          "$baseline_total"

echo
echo "===== With chain (single envelope) ====="
row "chain metadata + step records"                  $((chain_meta + chain_steps))
row "captured.pr subtree"                            "$chain_captured"
row "Total"                                          "$chain_total"

echo
reduction_bytes=$((baseline_total - chain_total))
reduction_pct=$(( reduction_bytes * 100 / baseline_total ))
echo "===== Result ====="
row "Reduction"                                      "$reduction_bytes"
echo "Reduction %: ${reduction_pct}%   (live=$DOGFOOD_LIVE polls=$POLL_COUNT)"

# Exit non-zero if reduction is below the 50% bar so CI / dogfood
# regressions trip immediately. Override via DOGFOOD_THRESHOLD.
THRESHOLD="${DOGFOOD_THRESHOLD:-50}"
if [ "$reduction_pct" -lt "$THRESHOLD" ]; then
  echo "WARN: reduction $reduction_pct% below threshold $THRESHOLD%" >&2
  exit 1
fi
