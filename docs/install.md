# Installing gaia

Four install paths, ordered roughly from "most users" to "fewest":

1. [**One-line installer**](#one-line-installer) — `curl | bash`
   wrapper that downloads, sha256-verifies, and installs the right
   prebuilt archive for your platform.
2. [**Homebrew**](#homebrew) (macOS + Linux): one-line install via
   `brew install`, formula auto-updated on every release.
3. [**Pre-built binary**](#pre-built-binary) — fastest path on any
   supported platform. One download, two binaries.
4. [**Build from source**](#build-from-source) — for contributors
   or anyone running a fork.

The container image (for `gaia-mcp --http` deployments) is a
separate concern — see [`deploy-mcp.md`](deploy-mcp.md).

## One-line installer

`scripts/install.sh` (in this repo, also served raw from the
canonical Forgejo URL) detects your OS + arch, downloads the
matching release archive plus its checksum file, sha256-verifies
before extracting, and drops both binaries into `~/.local/bin`
(overridable via `--prefix`). It also wires `~/.local/bin` into
the user's shell rc (bash, zsh, fish) idempotently — re-running
the installer never duplicates the line.

```bash
curl -fsSL https://raw.githubusercontent.com/stewartbrothers/gaia/main/scripts/install.sh \
  | TAG=v0.2.0 bash
```

Knobs:

- `TAG=vX.Y.Z` (env or `--tag vX.Y.Z`) — pin a release. Omit only
  if the API endpoint is reachable anonymously; on auth-gated
  forges TAG is required.
- `PREFIX=/path` (env or `--prefix /path`) — install destination.
  Defaults to `$HOME/.local/bin`. Use `/usr/local` (or any other
  system path) when you want a multi-user install; the script will
  ask sudo only when the dir isn't writable as the current user.
- `GH_TOKEN` / `GAIA_TOKEN` (env) — GitHub PAT. Not needed for
  public release downloads; only required if you've pointed
  `GAIA_DOWNLOAD_BASE` at a private host.
- `-v` / `--verbose` — print each download URL and the resolved
  sha256 instead of just the high-level `==>` markers.
- `GAIA_DOWNLOAD_BASE` (env) — override the release-asset base
  URL. Useful if you've mirrored the artifacts to your own host.

To uninstall: `rm $PREFIX/gaia $PREFIX/gaia-mcp` and
`sed -i '/# gaia$/d' ~/.zshrc` (or `~/.bashrc`/`~/.config/fish/config.fish`).
The trailing `# gaia` marker on the rc edit is the anchor.

## Homebrew

`gaia` ships a Homebrew formula from a dedicated tap repo,
[`stewartbrothers/homebrew-gaia`](https://github.com/stewartbrothers/homebrew-gaia)
on GitHub (a push-mirror of the canonical Forgejo
`Gerwood/homebrew-gaia`, which is where goreleaser auto-bumps the
formula on each release):

```bash
brew tap stewartbrothers/gaia https://github.com/stewartbrothers/homebrew-gaia
brew install gaia
```

The `https://...` URL form is required because Homebrew defaults
to `github.com/<owner>/homebrew-<name>` — the explicit URL points
the tap at the correct repo. Both `gaia` and `gaia-mcp` land on
`$PATH` after the install.

> **Already tapped the old location?** Earlier releases served the
> formula from the main repo (`…/stewartbrothers/gaia`). Re-point
> the tap once:
>
> ```bash
> brew untap stewartbrothers/gaia
> brew tap stewartbrothers/gaia https://github.com/stewartbrothers/homebrew-gaia
> brew upgrade gaia
> ```

To upgrade:

```bash
brew update
brew upgrade gaia
```

The formula is **auto-updated on every release tag** by the
release workflow (see [`../RELEASING.md`](../RELEASING.md)) — when
a `v*` tag is pushed, goreleaser rewrites `Formula/gaia.rb` with
the new archive URL and `sha256`, then commits it to the
`homebrew-gaia` tap repo's `main`. So `brew upgrade gaia` always
pulls the latest tagged release.

### Verifying the install

```bash
gaia version
gaia-mcp --help
```

Both binaries ship in the formula's `install` step; if either is
missing on `$PATH`, something went wrong with the tap (file an
issue on the Forgejo repo).

## Pre-built binary

Each tagged release ships archives for the platforms operators
are most likely to deploy to:

| OS      | amd64 | arm64 |
|---------|-------|-------|
| Linux   | ✓     | ✓     |
| macOS   | ✓     | ✓     |
| Windows | ✓     | ✓     |

Each archive contains both `gaia` (CLI) and `gaia-mcp` (MCP
server) plus LICENSE, README, and the `docs/` tree.

```bash
# Replace v0.2.0 with the current tag, choose your arch.
TAG=v0.2.0
PLATFORM=linux_x86_64       # or darwin_arm64, windows_x86_64, …

curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/${TAG}/gaia_${TAG}_${PLATFORM}.tar.gz"
curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/${TAG}/gaia_${TAG}_checksums.txt"

sha256sum -c <(grep "${PLATFORM}" "gaia_${TAG}_checksums.txt")

tar -xzf "gaia_${TAG}_${PLATFORM}.tar.gz"
sudo install gaia gaia-mcp /usr/local/bin/

gaia version
gaia-mcp --help
```

For Windows, swap `tar.gz` for `zip`, `sha256sum -c` for
`Get-FileHash`, and skip the `sudo install` step (drop the
extracted binaries somewhere on `$Env:PATH`).

### Verifying the build

`gaia version` reports the tag name and the abbreviated commit:

```json
{
  "schema_version": "1.0",
  "data": {
    "version": "v0.1.0",
    "commit": "abc1234",
    "go_version": "go1.23.4"
  }
}
```

Same string the Dockerfile injects into the container, so a
binary from a release archive and one from a `gaia-mcp:v0.1.0`
docker image report identical metadata — useful for confirming a
prod incident reproduces against the same build.

## Build from source

For contributors and operators running a fork:

```bash
git clone https://github.com/stewartbrothers/gaia.git
cd gaia
make build         # → bin/gaia, bin/gaia-mcp
```

The Makefile injects `git describe` and the short SHA via
`-ldflags`, so `gaia version` reports the exact build state
(including a `-dirty` suffix when there are uncommitted changes).

For a release-shaped local build:

```bash
make release-snapshot   # → dist/gaia_v0.0.1-snapshot+SHORTSHA_*.tar.gz
```

That's the same goreleaser invocation the workflow runs on tag
push, with `--snapshot` for a snapshot-flavored version string
and `--skip=publish` so nothing leaves the laptop.

Requires `goreleaser` itself; install with:

```bash
go install github.com/goreleaser/goreleaser/v2@v2.4.5
```

(Pin to the version listed here so local snapshots match what CI
produces.)

## Auth setup

After installing, configure forge access:

```bash
# Forgejo or Gitea — replace with your instance's API URL:
gaia auth forgejo https://your-forge.example.com/api/v1
# Paste a PAT with scopes: read:user, read:repository,
# write:issue, write:pull_request

# GitHub:
gaia auth gh
# Paste a GitHub fine-grained PAT
```

Both store credentials in `~/.config/gaia/credentials.yaml` with
mode 0600. See [`auth.md`](auth.md) for the full credential
resolution story (env vars, project-local credentials, multi-host
setups).

After auth, `gaia whoami` confirms the configured token works:

```bash
$ gaia whoami
{"data": {"login": "alice", "host": "your-forge.example.com"}}
```

## What about apt / Scoop?

Homebrew is supported (above). `apt` and Scoop are on the roadmap
if there's demand. Until those land, the pre-built binary covers
Debian/Ubuntu and Windows; Homebrew covers macOS and Linux for
users who already have it.
