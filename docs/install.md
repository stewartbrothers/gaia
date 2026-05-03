# Installing gaia

Four install paths, ordered roughly from "most users" to "fewest":

1. [**Homebrew**](#homebrew) (macOS + Linux): one-line install via
   `brew install`, formula auto-updated on every release.
2. [**Pre-built binary**](#pre-built-binary) — fastest path on any
   supported platform. One download, two binaries.
3. [**`go install`**](#go-install) — for Go developers who already
   have the toolchain set up. Always builds from latest `main`.
4. [**Build from source**](#build-from-source) — for contributors
   or anyone running a fork.

The container image (for `gaia-mcp --http` deployments) is a
separate concern — see [`deploy-mcp.md`](deploy-mcp.md).

## Homebrew

`gaia` ships a Homebrew formula in this repo, served as a tap
straight off the canonical Forgejo instance:

```bash
brew tap Gerwood/gaia https://github.com/stewartbrothers/gaia
brew install gaia
```

The `https://...` URL form is required because Homebrew defaults
to `github.com/<owner>/homebrew-<name>` — the explicit URL points
the tap at the Forgejo repo instead. Both `gaia` and `gaia-mcp`
land on `$PATH` after the install.

After the [GitHub mirror](mirroring.md) is live, the tap also
works against the mirror:

```bash
brew tap Gerwood/gaia https://github.com/stewartbrothers/gaia
brew install gaia
```

(Same formula; the `Formula/gaia.rb` file is committed to both
sides via the mirror push.)

To upgrade:

```bash
brew update
brew upgrade gaia
```

The formula is **auto-updated on every release tag** by the
release workflow (see [`../RELEASING.md`](../RELEASING.md)) — when
a `v*` tag is pushed, goreleaser rewrites `Formula/gaia.rb` with
the new archive URL and `sha256`, then commits to `main`. So
`brew upgrade gaia` always pulls the latest tagged release.

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
# Replace v0.1.0 with the current tag, choose your arch.
TAG=v0.1.0
PLATFORM=linux_x86_64       # or darwin_arm64, windows_x86_64, …

curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/${TAG}/gaia_${TAG}_${PLATFORM}.tar.gz"
curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/${TAG}/gaia_${TAG}_checksums.txt"

# Verify before extracting — the checksums file is signed by the
# release workflow's commit signature on Forgejo.
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

## `go install`

For developers who have Go 1.23+ on PATH:

```bash
go install github.com/stewartbrothers/gaia/cmd/gaia@latest
go install github.com/stewartbrothers/gaia/cmd/gaia-mcp@latest
```

This installs both binaries to `$(go env GOPATH)/bin/`. **Note**:
`go install` doesn't run the goreleaser ldflags injection, so
`gaia version` reports the Go module version (e.g. `v0.1.0`) but
the `commit` field stays as the default `unknown`. For a build
that reports the commit too, use the pre-built binary or build
from source.

`@latest` resolves to the most recent semver tag. Pin to a tag
explicitly for reproducibility:

```bash
go install github.com/stewartbrothers/gaia/cmd/gaia@v0.1.0
```

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

For a release-shaped local build (matching what `go install` and
the release workflow produce):

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
gaia auth forgejo https://your-forge.example.com/api/v1
# Paste a Forgejo PAT with scopes: read:user, read:repository,
# write:issue, write:pull_request

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

Homebrew is supported (above). `apt` and Scoop tracked under epic
#5 (issue #49) — only if there's demand. Until those land, the
pre-built binary covers Debian/Ubuntu and Windows; Homebrew
covers macOS and Linux for users who already have it.
