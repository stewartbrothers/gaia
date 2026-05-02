# Releasing gaia

This doc covers the version convention + the cut-a-release procedure.

## Version convention

gaia follows [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).

### `0.x.y` (current era)

While we're on `0.x.y`, the standard SemVer carve-out applies:
**breaking changes to the public surface may land at minor bumps.**
Specifically:

- `0.MAJOR.0` — substantial new functionality, may include breaking
  changes to CLI flag names, MCP tool names, envelope shape, exit
  codes, config file format, or stored credentials shape. Breaking
  changes are listed prominently in the CHANGELOG.
- `0.x.PATCH` — backward-compatible bug fixes only. Any user with the
  same `0.x` minor can take patch bumps without reading release notes.

Pre-releases use `-rc.N` (release candidate) or `-beta.N` suffixes:
`v0.2.0-rc.1`, `v0.2.0-beta.2`. These are tagged identically to
stable releases but the workflow flags them as pre-release in the
Forgejo UI.

### `1.0.0` and beyond (future)

`1.0.0` is the stability commitment. From there:

- **MAJOR**: breaking changes to the public surface. Reserved.
- **MINOR**: backward-compatible additions. New CLI commands, new
  MCP tools, new optional fields in the envelope, new exit codes.
- **PATCH**: backward-compatible bug fixes. No new features, no
  surface change.

The "public surface" at `1.0.0` will explicitly include:

1. CLI command + subcommand names (`gaia issue list`, `gaia pr create`)
2. CLI flag names + types (`--repo`, `--format`, `--fields`)
3. Output envelope shape (`schema_version`, `data`, `_truncated`,
   `_next_cursor`)
4. Exit code matrix
5. MCP tool names + argument shapes
6. Config file YAML shape (`profiles`, `default_profile`, `default_repo`)
7. Stored credential file YAML shape

Implementation details (Go API surface, internal paths, how a
particular tool is wired) are NOT public surface and may change
between PATCH versions.

## Cut-a-release procedure

### 0. Make sure main is green

```bash
git checkout main
git pull --ff-only
make fmt vet lint cover build
```

The `cover` target runs the same suite CI runs. If anything's red,
stop here.

### 1. Update CHANGELOG.md

Move the relevant entries from `[Unreleased]` to a new
`[X.Y.Z] — YYYY-MM-DD` section. Group changes under:

- `Added` — new features
- `Changed` — changes to existing behavior (breaking flagged)
- `Deprecated` — soon-to-be-removed surface
- `Removed` — what went away
- `Fixed` — bug fixes
- `Security` — security-relevant changes

Reference issue/PR numbers (`#42`, `PR #100`) so a reader can trace
to context.

Update the comparison links at the bottom of the file.

### 2. Open a release-prep PR

```bash
git checkout -b release/vX.Y.Z
git add CHANGELOG.md
git commit -m "release: vX.Y.Z"
git push -u origin release/vX.Y.Z
gaia pr create \
  --title "release: vX.Y.Z" \
  --base main \
  --head release/vX.Y.Z \
  --body "..."
```

Get it reviewed + merged. The CHANGELOG section is the canonical
release notes that the workflow attaches to the Forgejo release.

### 3. Tag

After the release-prep PR merges:

```bash
git checkout main
git pull --ff-only
git tag -a vX.Y.Z -m "release: vX.Y.Z"
git push origin vX.Y.Z
```

The annotated tag (`-a`) carries metadata git describes from. The
push triggers the `.forgejo/workflows/release.yml` workflow.

### 4. Workflow runs

`release.yml` does:

1. `actions/checkout@v4` with `fetch-depth: 0` (goreleaser needs
   full history for changelog).
2. `actions/setup-go@v5` with Go 1.23.
3. Shell-installs `goreleaser/v2@v2.4.5` (third-party actions
   don't mirror to code.forgejo.org, so we install via `go install`).
4. Runs `goreleaser release --clean --skip=publish`.
5. Builds `bin/gaia` from this tag's source.
6. Runs `gaia release publish "${TAG}" --asset 'dist/*' --notes-from CHANGELOG.md`,
   which creates the release record (Forgejo doesn't auto-create
   on tag push) and uploads each asset.

If the workflow fails after the tag is pushed, the tag stays — the
release may be empty or partial. Re-running the workflow (Forgejo
UI → Actions → re-run) re-attaches missing assets since
`gaia release publish` is idempotent on the create step. Local
recovery:

```bash
git checkout vX.Y.Z
make release-snapshot              # NB: this writes -snapshot+SHA tags
# — for a real release, run goreleaser without --snapshot:
goreleaser release --clean --skip=publish
gaia release publish vX.Y.Z \
  --asset 'dist/*.tar.gz' \
  --asset 'dist/*.zip' \
  --asset 'dist/*checksums.txt' \
  --notes-from CHANGELOG.md
```

Equivalent to what the workflow runs, just from your laptop.

### 5. Verify

```bash
# From the Forgejo release page, download a binary for your platform:
curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/vX.Y.Z/gaia_vX.Y.Z_linux_x86_64.tar.gz"
tar -xzf gaia_vX.Y.Z_linux_x86_64.tar.gz
./gaia version
# → reports vX.Y.Z + abbreviated commit + go version
```

If `gaia version` reports the right tag, the build pipeline is
working end-to-end.

### 6. Post-release

Add a fresh `[Unreleased]` section to `CHANGELOG.md` for follow-up
work to accumulate against.

## Hot-fix releases

If a critical bug needs to ship faster than a normal cycle:

1. Branch from the tag: `git checkout -b hotfix/vX.Y.Z+1 vX.Y.Z`
2. Cherry-pick or write the fix.
3. Update CHANGELOG.md under a new `[X.Y.Z+1]` section.
4. Open a PR targeting `main` (and once merged, manually merge the
   same fix into any other supported branches if applicable).
5. Tag from main as usual.

We don't currently ship a release-branch model — every release is
cut from `main`. If we adopt LTS branches in the future, this
section will grow.

## Pre-release tags

```bash
git tag -a vX.Y.Z-rc.1 -m "release candidate 1 for vX.Y.Z"
git push origin vX.Y.Z-rc.1
```

The Forgejo workflow currently doesn't distinguish pre-releases from
stable. If we need that flag set explicitly, extend the workflow's
upload step to PATCH the release with `prerelease: true` after
attaching assets. (Filed as a future TODO; not blocking.)

## Required Forgejo secret

`FORGEJO_RELEASE_TOKEN` — repo-scoped token with `write:repository`
scope. Set via Repository Settings → Secrets → Add Secret.

Without this the workflow's upload step 401s and the release ends
up empty. Workflow logs point at the fix.

## Container image publishing

Not yet wired into the release workflow — local-build only via the
project's `Dockerfile`. Push-to-registry (GHCR or ttl.sh or your own
registry) tracked under the broader Distribution epic (#5,
specifically #50). When that lands, this doc will gain a "container
image" section parallel to the binaries section above.
