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

After the release-prep PR merges, the tag step is wrapped in a
helper script that runs the same gate CI runs and pushes the tag:

```bash
git checkout main
git pull --ff-only
./scripts/cut-release.sh vX.Y.Z
```

The script:

1. Refuses to cut a release if the working tree is dirty, you're
   not on `main`, or local `main` differs from `origin/main`.
2. Refuses if `CHANGELOG.md` doesn't have a `[X.Y.Z]` section
   (i.e. the release-prep PR step was skipped).
3. Refuses if the tag already exists locally or on the remote.
4. Runs `make fmt vet lint cover build` — same gate CI runs.
5. Creates an annotated tag (`-a`) and pushes to `origin`.
6. Prints a follow-up checklist + the Forgejo Actions URL.

Manual fallback (if the script blocks for a reason you've decided
to override):

```bash
git tag -a vX.Y.Z -m "release: vX.Y.Z"
git push origin vX.Y.Z
```

Either path triggers the `.forgejo/workflows/release.yml` workflow.

### 4. Workflow runs

`release.yml` does:

1. `actions/checkout@v4` with `fetch-depth: 0` (goreleaser needs
   full history for changelog).
2. **Verify the tag points at `main`.** Forgejo Actions doesn't
   gate which branch a tag was created on, so the workflow walks
   the commit graph and refuses to release from a feature-branch
   tag. Caught early so the goreleaser run doesn't burn 5 minutes
   on artifacts that can't ship.
3. `actions/setup-go@v5` with Go 1.26.
4. Shell-installs `goreleaser/v2@v2.4.5` (third-party actions
   don't mirror to code.forgejo.org, so we install via `go install`).
5. Runs `goreleaser release --clean`. The `brews:` block (#49)
   writes a commit bumping `Formula/gaia.rb` to the new tag's url
   + sha256 and pushes it to `main` (gated on
   `GORELEASER_TAP_DEPLOY_KEY` being configured). The Forgejo
   release record itself is opted out via `release: disable: true`
   in `.goreleaser.yml` — `gaia release publish` (step 7) creates
   it. `--skip=publish` is **not** passed because that flag would
   also disable the brew tap push (the brew pipe runs in
   goreleaser's publish phase). See #260.
6. Builds `bin/gaia` from this tag's source.
7. Runs `gaia release publish "${TAG}" --asset 'dist/*' --notes-from CHANGELOG.md`,
   which creates the release record (Forgejo doesn't auto-create
   on tag push) and uploads each asset.
8. **If `GH_RELEASE_TOKEN` is configured**, creates a matching
   GitHub release and uploads the same artifacts to
   `github.com/stewartbrothers/gaia` — this is the public download
   point for the one-line installer.
9. **If `GITHUB_MIRROR_SSH_KEY` is configured (#47)**, pushes the
   tag to `github.com/stewartbrothers/gaia`.

Each step that can fail (goreleaser, build, publish, mirror push)
captures stderr to a file and re-emits it as `::error::` annotations,
so the workflow run page surfaces the specific failure rather than
burying it in a wall of output.

If the workflow fails after the tag is pushed, the tag stays — the
release may be empty or partial. Re-running the workflow (Forgejo
UI → Actions → re-run) is **idempotent**:

- `gaia release publish` no-ops on existing release records.
- The Homebrew formula bump is a regular `git push`, also idempotent.
- The mirror tag push converges on canonical state.

Local recovery for the rare case the workflow can't be re-run:

```bash
git checkout vX.Y.Z
make release-snapshot              # NB: this writes -snapshot+SHA tags
# — for a real release, run goreleaser without --snapshot.
# Don't pass --skip=publish: that also skips the brew tap push.
# `release: disable: true` in .goreleaser.yml already opts out of
# the Forgejo release-record creation.
goreleaser release --clean
gaia release publish vX.Y.Z \
  --asset 'dist/*.tar.gz' \
  --asset 'dist/*.zip' \
  --asset 'dist/*checksums.txt' \
  --notes-from CHANGELOG.md
```

Equivalent to what the workflow runs, just from your laptop.

### 5. Verify

Run through the checklist:

- [ ] **Forgejo release page** shows `vX.Y.Z` with all six archives
      (linux/darwin/windows × amd64/arm64) plus the checksums file.
      Download one and run `./gaia version` — should report the new
      tag.
- [ ] **`Formula/gaia.rb` on `main`** has been bumped to the new
      tag's url + sha256 (the `release: bump Homebrew formula to
      vX.Y.Z` commit, signed by `gaia-release-bot`). If
      `GORELEASER_TAP_DEPLOY_KEY` was unset, this step silently
      skips — re-run after configuring the secret.
- [ ] **GitHub release** has the new tag and all artifacts at
      `github.com/stewartbrothers/gaia/releases/tag/vX.Y.Z`. If
      `GH_RELEASE_TOKEN` was unset, the workflow skips this step
      with a notice.
- [ ] **One-line installer** works: `curl -fsSL https://raw.githubusercontent.com/stewartbrothers/gaia/main/scripts/install.sh | TAG=vX.Y.Z bash`
- [ ] **Container image** published: `docker pull ghcr.io/stewartbrothers/gaia-mcp:vX.Y.Z` and `docker pull ghcr.io/stewartbrothers/gaia-mcp:latest` both succeed. If `GH_RELEASE_TOKEN` lacked `write:packages`, the workflow skips with a notice.
- [ ] **`brew upgrade gaia`** on a tap-installed Mac picks up the
      new version: `brew upgrade gaia && gaia version`. (Skip if
      the formula bump didn't run.)

Manual binary check (any platform):

```bash
curl -fsSLO "https://github.com/stewartbrothers/gaia/releases/download/vX.Y.Z/gaia_vX.Y.Z_linux_x86_64.tar.gz"
tar -xzf gaia_vX.Y.Z_linux_x86_64.tar.gz
./gaia version
# → reports vX.Y.Z + abbreviated commit + go version
```

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

## Required Forgejo secrets

The release workflow uses up to three secrets, all configured via
Repository Settings → Secrets → Add Secret:

- **`FORGEJO_RELEASE_TOKEN`** — repo-scoped Forgejo token with
  `write:repository` scope. Used by `gaia release publish` to
  create the release record and upload artifacts. **Required.**
  Without it the upload step 401s and the release ends up empty.

- **`GORELEASER_TAP_DEPLOY_KEY`** — SSH private key matching a
  deploy key with **write** access on this repo. Used by the
  `brews:` block (#49) to push the `Formula/gaia.rb` bump to
  `main` after each tagged release. **Optional**; if absent, the
  Homebrew formula doesn't get auto-updated and tap users stay
  on the previous tag until the next manual update.

- **`GH_RELEASE_TOKEN`** — GitHub **classic** PAT with scopes
  `public_repo` (create releases + upload assets) and
  `write:packages` (push container image to GHCR; includes
  `read:packages`). Fine-grained PATs cannot push to GHCR.
  Used by the release workflow to create the GitHub release,
  upload artifacts, and push the container image.
  **Strongly recommended** — the public one-line installer and
  `docker pull` both depend on it.

- **`GITHUB_MIRROR_SSH_KEY`** — SSH private key matching a deploy
  key with **write** access on `github.com/stewartbrothers/gaia`
  (the public mirror). Used by both `mirror.yml` (#47) and the
  release workflow's mirror-push step. **Optional**; if absent,
  tags don't auto-push to GitHub (but the release upload step
  above doesn't need it).

The optional secrets gate independently — configure the ones you
need, leave the others unset. Workflow logs surface a `notice`
line when a step is skipped due to a missing secret so you can
tell at a glance which subsystems are wired.

## Container image publishing

Not yet wired into the release workflow — local-build only via the
project's `Dockerfile`. Push-to-registry (GHCR, the Forgejo package
registry, or ttl.sh) is a future TODO; when that lands, this doc
will gain a "container image" section parallel to the binaries
section above.
