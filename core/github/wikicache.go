package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
)

// GitHub wikis are not a REST resource — they're an independent git
// repository at `https://github.com/{owner}/{repo}.wiki.git`. To
// implement the Provider's wiki surface we keep a per-repo working
// clone under the user's cache dir, refresh it on a TTL, and push
// edits/deletes back through the remote.
//
// Cache layout (mode 0700 throughout):
//
//	$XDG_CACHE_HOME/gaia/wikis/
//	  └── {owner}/
//	        └── {repo}/        ← the working clone
//	              ├── .git/
//	              ├── Home.md
//	              └── ...
//
// Falls back to ~/.cache/gaia/wikis when XDG_CACHE_HOME is unset
// (matches the XDG default specified in the basedir spec).
//
// TTL: read paths refresh the clone if it's older than the TTL,
// otherwise serve the on-disk copy unchanged. Stale-while-revalidate
// would be a nicer story but adds complexity we don't need yet —
// `git pull --ff-only` on a 5-min interval is well-behaved against
// GitHub.

const (
	// defaultWikiCacheTTL is how long a clone is served without a
	// `git pull`. Tuned for "interactive agent activity in the same
	// session sees its own writes immediately, agents starting fresh
	// re-fetch within 5 minutes" — short enough that stale views are
	// rare, long enough that a chatty session doesn't hammer git.
	defaultWikiCacheTTL = 5 * time.Minute

	// wikiBranch is the default branch name on every GitHub wiki.
	// GitHub does not let users rename it; wikis ship with `master`
	// regardless of the parent repo's default branch. (Verified
	// 2025+ — GitHub's wiki feature predates the main→default rename
	// and stays on master to avoid breaking existing clone URLs.)
	wikiBranch = "master"
)

// wikiCache is the cache state shared by all wiki operations. It's
// safe to construct one per Provider (no connection pool, no shared
// auth state — each operation invokes git directly).
type wikiCache struct {
	// root is the directory under which {owner}/{repo}/ subtrees live.
	root string
	// ttl is the max age a clone may be served without a `git pull`.
	ttl time.Duration
	// token is the GitHub PAT used for clone/push URLs. Empty means
	// "anonymous git" — works for read of public wikis, fails on push.
	token string
}

// newWikiCache builds a cache rooted at the user's XDG cache dir (or
// the documented fallback). Returns an error only if the cache root
// can't be created.
func newWikiCache(token string) (*wikiCache, error) {
	root, err := defaultWikiCacheRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "create wiki cache root")
	}
	return &wikiCache{
		root:  root,
		ttl:   defaultWikiCacheTTL,
		token: token,
	}, nil
}

// defaultWikiCacheRoot resolves $XDG_CACHE_HOME/gaia/wikis with the
// fallback to ~/.cache/gaia/wikis. Mirrors the lookup order in the
// XDG basedir spec.
func defaultWikiCacheRoot() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "gaia", "wikis"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", exitcode.Wrap(err, exitcode.Generic, "resolve home dir for wiki cache")
	}
	return filepath.Join(home, ".cache", "gaia", "wikis"), nil
}

// ensureClone returns the path to a checked-out wiki clone for
// {owner}/{repo}, cloning if absent and refreshing if older than the
// TTL. Callers may safely modify the returned working tree — the
// next ensureClone call observes the modifications until a refresh
// fast-forwards them away.
func (c *wikiCache) ensureClone(ctx context.Context, owner, repo, remote string) (string, error) {
	ownerDir := filepath.Join(c.root, owner)
	if err := os.MkdirAll(ownerDir, 0o700); err != nil {
		return "", exitcode.Wrap(err, exitcode.Generic, "create wiki cache owner dir")
	}
	repoDir := filepath.Join(ownerDir, repo)
	gitDir := filepath.Join(repoDir, ".git")

	if _, err := os.Stat(gitDir); err != nil {
		if !os.IsNotExist(err) {
			return "", exitcode.Wrap(err, exitcode.Generic, "stat wiki clone")
		}
		// No clone yet — shallow clone now.
		if err := c.clone(ctx, remote, repoDir); err != nil {
			return "", err
		}
		return repoDir, nil
	}

	// Clone exists. Refresh if older than the TTL.
	stale, err := c.olderThanTTL(repoDir)
	if err != nil {
		return "", err
	}
	if stale {
		if err := c.refresh(ctx, repoDir); err != nil {
			return "", err
		}
	}
	return repoDir, nil
}

// olderThanTTL returns true if the clone's last refresh was further in
// the past than the cache's TTL. The mtime of `.git/FETCH_HEAD` is
// updated by `git pull`, so it's the most accurate "last refresh"
// signal. Falls back to `.git/HEAD` for fresh clones that haven't
// been pulled yet.
func (c *wikiCache) olderThanTTL(repoDir string) (bool, error) {
	for _, name := range []string{"FETCH_HEAD", "HEAD"} {
		info, err := os.Stat(filepath.Join(repoDir, ".git", name))
		if err == nil {
			return time.Since(info.ModTime()) > c.ttl, nil
		}
		if !os.IsNotExist(err) {
			return false, exitcode.Wrap(err, exitcode.Generic, "stat clone marker")
		}
	}
	// No marker file at all — treat as stale to force a refresh.
	return true, nil
}

// clone runs `git clone --depth 1 --branch master <remote> <dir>`.
// Stripping history keeps the cache small; we never need the wiki's
// full git log because the Provider methods only show the latest
// state of each page.
func (c *wikiCache) clone(ctx context.Context, remote, dir string) error {
	args := []string{"clone", "--depth", "1", "--branch", wikiBranch, remote, dir}
	if err := c.runGit(ctx, "", args...); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "clone wiki")
	}
	return nil
}

// refresh fetches origin/master at depth 1 and hard-resets the working
// tree to it. We don't need to preserve local history — the cache only
// holds the latest page state, and any uncommitted local edits are
// either (a) freshly written by an in-flight EditWikiPage, in which
// case the caller refreshes BEFORE writing not after, or (b) leftover
// from a crashed write, in which case throwing them away is correct.
// `--ff-only pull` doesn't work here because GitHub's wiki repo can
// have history rewrites (e.g. squash-on-conflict from the web UI), and
// we'd rather take the upstream verbatim than fight to fast-forward.
func (c *wikiCache) refresh(ctx context.Context, dir string) error {
	if err := c.runGit(ctx, dir, "fetch", "--depth", "1", "origin", wikiBranch); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "refresh wiki cache (fetch)")
	}
	if err := c.runGit(ctx, dir, "reset", "--hard", "origin/"+wikiBranch); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "refresh wiki cache (reset)")
	}
	// Bump FETCH_HEAD mtime so the TTL check in olderThanTTL sees a
	// fresh marker (some git versions don't touch FETCH_HEAD on a
	// no-op fetch).
	now := time.Now()
	_ = os.Chtimes(filepath.Join(dir, ".git", "FETCH_HEAD"), now, now)
	return nil
}

// commitAndPush stages every change in the working tree, commits with
// the given message, and pushes back to origin/master. A push failure
// is a hard error: callers must treat the cache as poisoned for that
// repo (the next refresh will hit non-fast-forward upstream and fail
// loudly). We don't auto-clean the cache here because the caller has
// the context to know whether it's safe.
func (c *wikiCache) commitAndPush(ctx context.Context, dir, message string) error {
	// Use a stable identity for commits made by gaia so the upstream
	// log is readable. Both env vars and `-c user.x` would work; the
	// command-line config is more localised.
	gitArgs := func(extra ...string) []string {
		base := []string{
			"-c", "user.email=gaia@gaia.local",
			"-c", "user.name=gaia",
		}
		return append(base, extra...)
	}

	if err := c.runGit(ctx, dir, gitArgs("add", "-A")...); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "git add wiki change")
	}
	// `git commit` returns non-zero when there's nothing to stage; we
	// only want to fail on real failures, so probe staged status first.
	staged, err := c.hasStagedChanges(ctx, dir)
	if err != nil {
		return err
	}
	if !staged {
		// No-op: caller's modification produced no diff vs the cache.
		// This isn't a write at all; nothing to push.
		return nil
	}
	if err := c.runGit(ctx, dir, gitArgs("commit", "-m", message)...); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "commit wiki change")
	}
	if err := c.runGit(ctx, dir, "push", "origin", wikiBranch); err != nil {
		return exitcode.Wrap(err, exitcode.Generic, "push wiki change")
	}
	return nil
}

// hasStagedChanges returns true if `git diff --cached --quiet` reports
// staged content. Used to skip empty commits cleanly.
func (c *wikiCache) hasStagedChanges(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")
	cmd.Dir = dir
	cmd.Env = c.gitEnv()
	err := cmd.Run()
	if err == nil {
		return false, nil // exit 0 = no diff
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil // exit 1 = there is a diff
	}
	return false, exitcode.Wrap(err, exitcode.Generic, "diff staged wiki changes")
}

// runGit invokes git with the supplied args, optionally inside dir
// (use "" for "no chdir", e.g. clone). stderr is captured into the
// returned error so failures are diagnosable; stdout is discarded
// (none of our git invocations consume it).
func (c *wikiCache) runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = c.gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w (output: %s)", strings.Join(args, " "), err, scrubToken(string(out), c.token))
	}
	return nil
}

// gitEnv builds the env for git invocations. Notably:
//
//   - GIT_TERMINAL_PROMPT=0 prevents an interactive prompt if a remote
//     auth fails (we want a fast hard-error in that case).
//   - GIT_ASKPASS=/bin/echo suppresses any password helper invocation.
//
// The token is embedded directly in the remote URL, so no env-based
// credential plumbing is needed.
func (c *wikiCache) gitEnv() []string {
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/echo")
	return env
}

// scrubToken redacts the PAT from any captured git output before it
// surfaces in an error message. Defence-in-depth: git itself rarely
// echoes credentials, but a misbehaved helper or proxy could.
func scrubToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "<redacted>")
}

// wikiRemoteURL returns the authenticated clone/push URL for a GitHub
// wiki. Empty token → unauthenticated (works for read of public
// wikis, fails on push — the caller's commitAndPush surfaces that as
// a hard error).
func wikiRemoteURL(host, owner, repo, token string) string {
	if host == "" {
		host = "github.com"
	}
	if token == "" {
		return fmt.Sprintf("https://%s/%s/%s.wiki.git", host, owner, repo)
	}
	return fmt.Sprintf("https://x-access-token:%s@%s/%s/%s.wiki.git", token, host, owner, repo)
}
