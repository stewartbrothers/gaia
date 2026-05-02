#!/usr/bin/env bash
# scripts/dogfood-compare.sh — emit a side-by-side byte/token comparison
# of `gaia` against the equivalent `tea` (where present) and raw `curl`
# calls. Run after building the binary; results inform whether each
# command is shaping output the way agents need it.
#
# Usage:
#   GIT_FORGE_GITEA_TOKEN=... ./scripts/dogfood-compare.sh
#   REPO=Gerwood/gaia API=https://your-forge.example.com/api/v1 \
#     ./scripts/dogfood-compare.sh
#
# A "token" estimate uses the conventional bytes/4 heuristic so the
# numbers don't require a tokenizer to reproduce; treat it as
# approximate. Lower is better for the agent's context budget.

set -euo pipefail

REPO="${REPO:-Gerwood/gaia}"
API="${API:-https://your-forge.example.com/api/v1}"
TOKEN="${TOKEN:-${GIT_FORGE_GITEA_TOKEN:-}}"
GAIA_BIN="${GAIA_BIN:-./bin/gaia}"
PR_NUMBER="${PR_NUMBER:-75}"
SEARCH_QUERY="${SEARCH_QUERY:-MVP}"

if [ -z "$TOKEN" ]; then
  echo "set GIT_FORGE_GITEA_TOKEN or TOKEN" >&2
  exit 1
fi
if [ ! -x "$GAIA_BIN" ]; then
  echo "gaia binary not found at $GAIA_BIN — run 'make build' first" >&2
  exit 1
fi

# Gaia reads FORGEJO_TOKEN by default; export it from $TOKEN so the
# child commands authenticate without us threading --token through
# every invocation. Same for GITHUB_TOKEN if anyone runs this against
# github.com later.
export FORGEJO_TOKEN="$TOKEN"

# run captures stdout from a command and reports its byte size + an
# estimated token count (bytes/4). Errors go to stderr; if the command
# fails the row reports the partial output we did capture.
run() {
  local label="$1"; shift
  local out bytes tokens
  out="$("$@" 2>/dev/null || true)"
  bytes=$(printf '%s' "$out" | wc -c | tr -d ' ')
  tokens=$(( bytes / 4 ))
  printf '%-40s\t%6d\t%6d\n' "$label" "$bytes" "$tokens"
}

# Some baseline gaia + curl invocations need short flags repeated; this
# helper just runs the commands. Tea is optional — if not on PATH, that
# row prints "(tea not installed)".
have_tea=0
if command -v tea >/dev/null 2>&1; then
  have_tea=1
fi

printf '%-40s\t%6s\t%6s\n' "COMMAND" "BYTES" "TOKENS≈"
echo "------------------------------------------------------------------------"

echo "=== whoami ==="
run "gaia whoami"                $GAIA_BIN --provider forgejo --api-url "$API" whoami
run "curl /user"                 curl -sS -H "Authorization: token $TOKEN" "$API/user"
echo

echo "=== issue list (state=open, default 30) ==="
run "gaia issue list"            $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" issue list --state open
run "gaia --fields number,title,state" \
                                 $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" --fields number,title,state issue list --state open
run "curl /issues"               curl -sS -H "Authorization: token $TOKEN" "$API/repos/$REPO/issues?type=issues&state=open&limit=30"
if [ $have_tea -eq 1 ]; then
  run "tea issues list"          tea issues list --login Dev --repo "$REPO" --output simple --state open
else
  printf '%-40s\t%6s\t%6s\n' "tea issues list" "(tea not installed)" "-"
fi
echo

echo "=== issue view (#1) ==="
run "gaia issue view 1"          $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" issue view 1
run "curl /issues/1"             curl -sS -H "Authorization: token $TOKEN" "$API/repos/$REPO/issues/1"
if [ $have_tea -eq 1 ]; then
  run "tea issues 1"             tea issues 1 --login Dev --repo "$REPO" --output simple
fi
echo

echo "=== pr list (state=all, default 30) ==="
run "gaia pr list"               $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" pr list --state all
run "gaia --fields number,title,state,head.ref,base.ref" \
                                 $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" --fields number,title,state,head.ref,base.ref pr list --state all
run "curl /pulls"                curl -sS -H "Authorization: token $TOKEN" "$API/repos/$REPO/pulls?state=all&limit=30"
if [ $have_tea -eq 1 ]; then
  run "tea pulls list"           tea pulls list --login Dev --repo "$REPO" --output simple --state all
fi
echo

echo "=== pr view ($PR_NUMBER, with CI) ==="
run "gaia pr view (no CI)"       $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" pr view "$PR_NUMBER"
run "gaia pr view --with-ci"     $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" pr view "$PR_NUMBER" --with-ci
run "curl /pulls/N"              curl -sS -H "Authorization: token $TOKEN" "$API/repos/$REPO/pulls/$PR_NUMBER"
if [ $have_tea -eq 1 ]; then
  run "tea pulls $PR_NUMBER"     tea pulls "$PR_NUMBER" --login Dev --repo "$REPO" --output simple
fi
echo

echo "=== pr diff ($PR_NUMBER) ==="
run "gaia pr diff (full)"        $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" pr diff "$PR_NUMBER"
run "gaia --fields path,status"  $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" --fields path,status pr diff "$PR_NUMBER"
run "curl /pulls/N.diff (raw)"   curl -sS -H "Authorization: token $TOKEN" "$API/repos/$REPO/pulls/$PR_NUMBER.diff"
echo

echo "=== pr comments ($PR_NUMBER) ==="
run "gaia pr comments (unified)" $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" pr comments "$PR_NUMBER"
# Naive equivalent: three separate raw fetches concatenated.
run "3x curl (issue+rev+inline)" bash -c "
  curl -sS -H 'Authorization: token $TOKEN' '$API/repos/$REPO/issues/$PR_NUMBER/comments'
  curl -sS -H 'Authorization: token $TOKEN' '$API/repos/$REPO/pulls/$PR_NUMBER/reviews'
  curl -sS -H 'Authorization: token $TOKEN' '$API/repos/$REPO/pulls/$PR_NUMBER/comments'
"
echo

echo "=== search ($SEARCH_QUERY) ==="
run "gaia search"                $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" search "$SEARCH_QUERY"
run "gaia --fields kind,number,title,repo" \
                                 $GAIA_BIN --provider forgejo --api-url "$API" --repo "$REPO" --fields kind,number,title,repo search "$SEARCH_QUERY"
run "curl /repos/o/r/issues?q="  curl -sS -H "Authorization: token $TOKEN" "$API/repos/$REPO/issues?q=$SEARCH_QUERY&limit=30"
echo

echo "Tip: re-run with REPO=, PR_NUMBER=, SEARCH_QUERY= overrides for"
echo "different baselines. Numbers go into docs/dogfood-comparison.md."
