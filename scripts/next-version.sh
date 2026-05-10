#!/usr/bin/env bash
# scripts/next-version.sh — advisory: compute the next SemVer tag
# from the conventional-commit subjects merged since the last tag.
#
# Usage:
#
#   ./scripts/next-version.sh                    # since the latest semver tag
#   ./scripts/next-version.sh --since vX.Y.Z     # explicit base tag
#   ./scripts/next-version.sh --strict           # fail on unclassified commits
#
# Output:
#
#   stdout : the proposed next version, prefixed with `v` (e.g., `v0.4.0`).
#   stderr : one line per classified commit + a final tally + the
#            rule that triggered the chosen bump.
#
# Exit codes:
#
#   0  — clean classification, next version printed on stdout.
#   1  — --strict and at least one commit subject couldn't be classified.
#   65 — no commits since the base tag (refuse to bump a no-op).
#   64 — usage error.
#
# Classification rules (matches the conventional-commits spec subset
# the rest of the project's commit messages use):
#
#   - feat:        / feat(scope):        → MINOR
#   - fix:         / fix(scope):         → PATCH-eligible
#   - docs:, test:, chore:, refactor:, perf:, style:, ci:, build:, release:
#                                         → PATCH-eligible
#   - <type>!: in the subject            → BREAKING; MINOR pre-1.0, MAJOR post-1.0
#   - BREAKING CHANGE: in the body       → BREAKING; same as above
#   - anything else                       → unclassified (warned; --strict fatals)
#
# The script is advisory — operators still pass the chosen tag to
# scripts/cut-release.sh manually. See RELEASING.md.

set -euo pipefail

STRICT=0
SINCE=""

while [ $# -gt 0 ]; do
  case "$1" in
    --strict)
      STRICT=1
      shift
      ;;
    --since)
      if [ $# -lt 2 ]; then
        echo "error: --since requires an argument" >&2
        exit 64
      fi
      SINCE="$2"
      shift 2
      ;;
    -h|--help)
      sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "error: unknown argument '$1'" >&2
      echo "usage: $0 [--strict] [--since vX.Y.Z]" >&2
      exit 64
      ;;
  esac
done

# Resolve the base tag. If --since wasn't given, find the latest
# semver-ish tag reachable from HEAD.
if [ -z "$SINCE" ]; then
  SINCE="$(git tag --sort=-v:refname --merged HEAD \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$' \
    | head -n 1 || true)"
  if [ -z "$SINCE" ]; then
    echo "error: no semver tag found reachable from HEAD; pass --since vX.Y.Z" >&2
    exit 64
  fi
fi

# Sanity-check the base.
if ! git rev-parse --verify --quiet "$SINCE" >/dev/null; then
  echo "error: base tag '$SINCE' does not exist" >&2
  exit 64
fi
if [[ ! "$SINCE" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)(-[A-Za-z0-9.]+)?$ ]]; then
  echo "error: base tag '$SINCE' does not match SemVer pattern vX.Y.Z[-PRERELEASE]" >&2
  exit 64
fi
BASE_MAJOR="${BASH_REMATCH[1]}"
BASE_MINOR="${BASH_REMATCH[2]}"
BASE_PATCH="${BASH_REMATCH[3]}"

# Walk the commit graph since the base tag. We classify both regular
# commits AND merge subjects — squash workflows put feat:/fix: on the
# merge commit; non-squash workflows put them on the feature-branch
# commits. Reading both covers either style without double-counting,
# since a merge commit doesn't repeat its parents' subjects.
#
# %H = full SHA, %s = subject, %b = body. Tab-separated, NUL-terminated
# so subjects with newlines (rare but possible) don't split rows.
#
# Read NUL-terminated entries into an array via a portable while-read
# loop. mapfile -d '' is bash 4.4+; macOS ships bash 3.2 by default.
LOG_ENTRIES=()
while IFS= read -r -d '' entry; do
  # git inserts a `\n` between commits even with %x00 in the format
  # string. That trailing newline becomes the leading newline of the
  # next entry under `read -d ''` — strip it so the tab-split below
  # doesn't get an empty first line.
  entry="${entry#$'\n'}"
  LOG_ENTRIES+=("$entry")
done < <(git log "${SINCE}..HEAD" --format='%H%x09%s%x09%b%x00' || true)

if [ "${#LOG_ENTRIES[@]}" -eq 0 ]; then
  echo "error: no commits between $SINCE and HEAD; nothing to release" >&2
  exit 65
fi

has_feat=0
has_breaking=0
has_fix_or_chore=0
unclassified=0

# Regex fragments. Conventional-commit type with optional (scope)
# and optional ! (breaking marker), followed by a colon.
TYPE_RE='^([a-zA-Z]+)(\([^)]*\))?(!)?:'

# Classification log accumulator (printed at end to stderr).
classify_lines=()

classify() {
  local sha="$1" subject="$2" body="$3"
  local short
  short="$(printf '%s' "$sha" | cut -c1-8)"

  # BREAKING CHANGE: footer / trailer in the body is always breaking.
  if grep -qE '^BREAKING CHANGE:' <<<"$body"; then
    has_breaking=1
    classify_lines+=("${short}  BREAKING (body)      ${subject}")
    return
  fi

  if [[ "$subject" =~ $TYPE_RE ]]; then
    local type="${BASH_REMATCH[1]}"
    local bang="${BASH_REMATCH[3]}"

    # type!: subject form forces breaking.
    if [ "$bang" = "!" ]; then
      has_breaking=1
      classify_lines+=("${short}  BREAKING (${type}!)    ${subject}")
      return
    fi

    case "$type" in
      feat)
        has_feat=1
        classify_lines+=("${short}  feat                 ${subject}")
        ;;
      fix|docs|test|chore|refactor|perf|style|ci|build|release)
        has_fix_or_chore=1
        classify_lines+=("${short}  ${type}             ${subject}")
        ;;
      *)
        unclassified=$((unclassified + 1))
        classify_lines+=("${short}  UNCLASSIFIED (${type}) ${subject}")
        ;;
    esac
    return
  fi

  # Plain merge subjects ("Merge pull request '...' (#NNN) ...") are
  # classified by recursing into the merged-PR title if we can find it.
  # Easier path: ignore plain merges entirely — the feature/fix commits
  # they bring in will have proper conventional subjects and get
  # classified on their own. Same for "Merge branch 'main' into ..." noise.
  if [[ "$subject" =~ ^Merge\ (pull\ request|branch|remote-tracking) ]]; then
    classify_lines+=("${short}  -merge-              ${subject}")
    return
  fi

  unclassified=$((unclassified + 1))
  classify_lines+=("${short}  UNCLASSIFIED         ${subject}")
}

for entry in "${LOG_ENTRIES[@]}"; do
  [ -z "$entry" ] && continue
  IFS=$'\t' read -r sha subject body <<<"$entry"
  classify "$sha" "$subject" "${body:-}"
done

# Decide the bump.
NEW_MAJOR="$BASE_MAJOR"
NEW_MINOR="$BASE_MINOR"
NEW_PATCH="$BASE_PATCH"
bump_rule=""

if [ "$has_breaking" -eq 1 ]; then
  if [ "$BASE_MAJOR" -eq 0 ]; then
    NEW_MINOR=$((BASE_MINOR + 1))
    NEW_PATCH=0
    bump_rule="MINOR (breaking change on 0.x per SemVer §4)"
  else
    NEW_MAJOR=$((BASE_MAJOR + 1))
    NEW_MINOR=0
    NEW_PATCH=0
    bump_rule="MAJOR (breaking change post-1.0)"
  fi
elif [ "$has_feat" -eq 1 ]; then
  NEW_MINOR=$((BASE_MINOR + 1))
  NEW_PATCH=0
  bump_rule="MINOR (one or more feat: commits)"
elif [ "$has_fix_or_chore" -eq 1 ]; then
  NEW_PATCH=$((BASE_PATCH + 1))
  bump_rule="PATCH (fix/chore/docs only)"
else
  echo "error: no classifiable commits since $SINCE; aborting" >&2
  printf '%s\n' "${classify_lines[@]}" >&2
  exit 1
fi

NEW_VERSION="v${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"

# Report.
{
  echo "Base:  $SINCE"
  echo "Range: ${SINCE}..HEAD"
  echo ""
  printf '%s\n' "${classify_lines[@]}"
  echo ""
  echo "Summary: feat=${has_feat} fix/chore=${has_fix_or_chore} breaking=${has_breaking} unclassified=${unclassified}"
  echo "Bump:    ${bump_rule}"
  echo "Next:    ${NEW_VERSION}"
} >&2

if [ "$STRICT" -eq 1 ] && [ "$unclassified" -gt 0 ]; then
  echo "error: --strict and ${unclassified} unclassified commit(s); fix the subjects or drop --strict" >&2
  exit 1
fi

echo "$NEW_VERSION"
