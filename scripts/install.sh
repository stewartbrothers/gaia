#!/usr/bin/env bash
# scripts/install.sh — one-line installer for prebuilt gaia + gaia-mcp binaries.
#
# Usage (the README + wiki Quick Start one-liner):
#
#   curl -fsSL https://github.com/stewartbrothers/gaia/raw/branch/main/scripts/install.sh \
#     | TAG=v0.2.0 bash
#
# Authenticated forges (e.g. a private Forgejo instance that gates
# all read access) need a token in the env. The script honours
# GITEA_TOKEN / FORGEJO_TOKEN / GAIA_TOKEN — first one set wins:
#
#   curl -fsSL .../install.sh | TAG=v0.2.0 GITEA_TOKEN=xxx bash
#
# Or with explicit flags (when not piping):
#
#   ./install.sh --tag v0.2.0 --prefix /usr/local --verbose
#
# What it does:
#
#   1. Detect OS (linux/darwin) + arch (x86_64/arm64).
#   2. Resolve the latest release tag from the API when TAG is not set
#      (only works if the API is reachable — see auth note above).
#   3. Download the matching tarball + checksums file, sha256-verify
#      before extracting.
#   4. Install `gaia` and `gaia-mcp` into --prefix (default
#      $HOME/.local/bin), creating the directory if needed.
#   5. Append a single idempotent PATH line to the user's shell rc
#      (bash/zsh/fish), guarded by a `# gaia` marker comment so re-runs
#      don't duplicate. Skipped when the prefix is already on PATH.
#   6. Run `gaia version` from the install location and print its
#      output as proof-of-life.
#
# What it does NOT do:
#
#   - Windows. (File an issue if you need it.)
#   - Auto-upgrade detection. (Re-running with a newer --tag does the
#     right thing — same binary path overwritten.)
#   - Uninstall. To remove: `rm $PREFIX/gaia $PREFIX/gaia-mcp` and
#     `sed -i '/# gaia$/d' ~/.bashrc` (or your shell rc).

set -euo pipefail

# ----------------------------------------------------------------------
# Defaults (overridable via flags or env)
# ----------------------------------------------------------------------

GAIA_REPO="${GAIA_REPO:-Gerwood/gaia}"
GAIA_API="${GAIA_API:-https://your-forge.example.com/api/v1}"
GAIA_DOWNLOAD_BASE="${GAIA_DOWNLOAD_BASE:-https://your-forge.example.com/${GAIA_REPO}/releases/download}"
TAG="${TAG:-}"
PREFIX="${PREFIX:-$HOME/.local/bin}"
VERBOSE="${VERBOSE:-0}"

# First non-empty token wins. Sent as `Authorization: token ...` to
# Forgejo/Gitea (matches gaia's own convention) and Bearer to GitHub.
TOKEN="${GITEA_TOKEN:-${FORGEJO_TOKEN:-${GAIA_TOKEN:-${GH_TOKEN:-${GITHUB_TOKEN:-}}}}}"

# ----------------------------------------------------------------------
# Output helpers
# ----------------------------------------------------------------------

err()  { printf '%s: %s\n' "install.sh" "$*" >&2; }
log()  { printf '==> %s\n' "$*"; }
vlog() { [ "$VERBOSE" = "1" ] && printf '    %s\n' "$*" || true; }
die()  { err "$*"; exit 1; }

# ----------------------------------------------------------------------
# Flag parsing
# ----------------------------------------------------------------------

while [ $# -gt 0 ]; do
  case "$1" in
    --tag)        TAG="${2:?--tag requires a value}"; shift 2 ;;
    --tag=*)      TAG="${1#*=}"; shift ;;
    --prefix)     PREFIX="${2:?--prefix requires a value}"; shift 2 ;;
    --prefix=*)   PREFIX="${1#*=}"; shift ;;
    -v|--verbose) VERBOSE=1; shift ;;
    -h|--help)
      sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) die "unknown argument: $1 (try --help)" ;;
  esac
done

# ----------------------------------------------------------------------
# Required tools
# ----------------------------------------------------------------------

need() { command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1"; }

need curl
need tar
need uname
need mktemp
if command -v sha256sum >/dev/null 2>&1; then
  SHA256_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  SHA256_CMD=(shasum -a 256)
else
  die "neither sha256sum nor shasum found"
fi

# Wrap curl so the auth header (when set) and a sane UA are applied
# uniformly. -fsSL: fail-on-error, silent, show-errors, follow redirects.
gcurl() {
  if [ -n "$TOKEN" ]; then
    curl -fsSL -H "Authorization: token $TOKEN" -A "gaia-installer" "$@"
  else
    curl -fsSL -A "gaia-installer" "$@"
  fi
}

# ----------------------------------------------------------------------
# OS / arch detection — must match goreleaser's archive naming.
# ----------------------------------------------------------------------

raw_os="$(uname -s)"
raw_arch="$(uname -m)"

case "$raw_os" in
  Linux)  os="linux" ;;
  Darwin) os="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) die "Windows is not supported by this installer; download the windows archive manually from the releases page" ;;
  *) die "unsupported OS: $raw_os" ;;
esac

case "$raw_arch" in
  x86_64|amd64)  arch="x86_64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) die "unsupported arch: $raw_arch" ;;
esac

platform="${os}_${arch}"
vlog "detected platform: $platform"

# ----------------------------------------------------------------------
# Resolve TAG if needed
# ----------------------------------------------------------------------

if [ -z "$TAG" ]; then
  log "resolving latest release tag from $GAIA_API"
  if ! latest_json="$(gcurl "$GAIA_API/repos/$GAIA_REPO/releases/latest" 2>&1)"; then
    die "failed to fetch latest release info — set TAG=vX.Y.Z to skip the lookup, or supply a token via GITEA_TOKEN/GAIA_TOKEN if the forge requires auth"
  fi
  TAG="$(printf '%s' "$latest_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$TAG" ] || die "could not extract tag_name from latest-release response"
fi

case "$TAG" in
  v*) ;;
  *)  die "tag '$TAG' must start with 'v' (e.g. v0.2.0)" ;;
esac

vlog "using tag: $TAG"

# ----------------------------------------------------------------------
# Download + verify
# ----------------------------------------------------------------------

archive="gaia_${TAG}_${platform}.tar.gz"
checksums="gaia_${TAG}_checksums.txt"
download_dir="$(mktemp -d -t gaia-install.XXXXXX)"
trap 'rm -rf "$download_dir"' EXIT

archive_url="$GAIA_DOWNLOAD_BASE/$TAG/$archive"
checksums_url="$GAIA_DOWNLOAD_BASE/$TAG/$checksums"

log "downloading $archive"
vlog "from $archive_url"
gcurl -o "$download_dir/$archive" "$archive_url" \
  || die "download failed: $archive_url (auth-gated forge? set GITEA_TOKEN)"

log "downloading $checksums"
vlog "from $checksums_url"
gcurl -o "$download_dir/$checksums" "$checksums_url" \
  || die "checksums download failed: $checksums_url"

log "verifying sha256"
expected="$(grep " $archive\$" "$download_dir/$checksums" | awk '{print $1}')"
[ -n "$expected" ] || die "no checksum line for $archive in $checksums"
actual="$( "${SHA256_CMD[@]}" "$download_dir/$archive" | awk '{print $1}')"
if [ "$expected" != "$actual" ]; then
  die "sha256 mismatch for $archive (expected $expected, got $actual) — refusing to install"
fi
vlog "sha256 ok: $actual"

# ----------------------------------------------------------------------
# Extract + install
# ----------------------------------------------------------------------

extract_dir="$download_dir/extract"
mkdir -p "$extract_dir"
tar -xzf "$download_dir/$archive" -C "$extract_dir"

[ -f "$extract_dir/gaia" ]     || die "extracted archive is missing 'gaia' binary"
[ -f "$extract_dir/gaia-mcp" ] || die "extracted archive is missing 'gaia-mcp' binary"

mkdir -p "$PREFIX"

# install -m 0755 sets perms atomically. If the dir isn't writable,
# retry with sudo (only when actually needed — don't gratuitously sudo).
install_one() {
  local src="$1" dst="$2"
  if [ -w "$(dirname "$dst")" ]; then
    install -m 0755 "$src" "$dst"
  else
    log "install requires sudo for $(dirname "$dst")"
    sudo install -m 0755 "$src" "$dst"
  fi
}

log "installing to $PREFIX"
install_one "$extract_dir/gaia"     "$PREFIX/gaia"
install_one "$extract_dir/gaia-mcp" "$PREFIX/gaia-mcp"

# ----------------------------------------------------------------------
# Shell rc PATH wiring — only edit when not already on PATH; gate
# duplicate entries on the `# gaia` marker.
# ----------------------------------------------------------------------

case ":$PATH:" in
  *":$PREFIX:"*)
    vlog "$PREFIX already on PATH — not editing shell rc"
    ;;
  *)
    user_shell_path="${SHELL:-}"
    user_shell="$(basename "${user_shell_path:-bash}")"
    case "$user_shell" in
      zsh)  rc_file="$HOME/.zshrc" ;;
      bash)
        # macOS Terminal launches login shells which read .bash_profile,
        # not .bashrc. Linux distros generally read .bashrc on
        # interactive non-login shells.
        if [ "$os" = "darwin" ] && [ -f "$HOME/.bash_profile" ]; then
          rc_file="$HOME/.bash_profile"
        else
          rc_file="$HOME/.bashrc"
        fi
        ;;
      fish) rc_file="$HOME/.config/fish/config.fish" ;;
      *)    rc_file="$HOME/.profile" ;;
    esac

    # Anchor the marker at end-of-line so future cleanup
    # (`sed -i '/# gaia$/d' "$rc_file"`) hits exactly this line.
    if [ "$user_shell" = "fish" ]; then
      path_line="set -gx PATH $PREFIX \$PATH  # gaia"
    else
      path_line="export PATH=\"$PREFIX:\$PATH\"  # gaia"
    fi

    mkdir -p "$(dirname "$rc_file")"
    touch "$rc_file"
    if grep -F "# gaia" "$rc_file" >/dev/null 2>&1; then
      vlog "rc file $rc_file already has a # gaia marker — not duplicating"
    else
      printf '\n# Added by gaia installer (%s)\n%s\n' "$TAG" "$path_line" >> "$rc_file"
      log "added $PREFIX to PATH in $rc_file"
      log "open a new shell or 'source $rc_file' to pick it up"
    fi
    ;;
esac

# ----------------------------------------------------------------------
# Proof-of-life — run the freshly installed binary directly so we
# don't depend on the rc edit being sourced yet.
# ----------------------------------------------------------------------

log "verifying installation"
"$PREFIX/gaia" version --format pretty 2>&1 || die "installed gaia did not run cleanly"

cat <<EOF

Installed:
  $PREFIX/gaia
  $PREFIX/gaia-mcp

Next steps:
  1. Open a new shell (or 'source' your rc file) so the PATH change applies.
  2. gaia auth forgejo https://your-forge.example.com/api/v1
  3. gaia whoami
EOF
