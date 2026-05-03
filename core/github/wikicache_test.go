package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initBareRemote creates a bare git repo with a single seed commit on
// the master branch (matches GitHub wiki convention) plus the requested
// page files. Returns the path to the bare repo, suitable for use as a
// `file://` clone URL.
func initBareRemote(t *testing.T, pages map[string]string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.wiki.git")
	seed := filepath.Join(root, "seed")

	mustRun(t, root, "git", "init", "--bare", "--initial-branch=master", bare)
	mustRun(t, root, "git", "init", "--initial-branch=master", seed)
	mustRun(t, seed, "git", "config", "user.email", "test@example")
	mustRun(t, seed, "git", "config", "user.name", "test")

	for name, body := range pages {
		path := filepath.Join(seed, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("seed write %s: %v", name, err)
		}
	}
	if len(pages) == 0 {
		// Empty repos can't be cloned; ensure at least one commit exists.
		if err := os.WriteFile(filepath.Join(seed, ".keep"), []byte(""), 0o600); err != nil {
			t.Fatalf("seed .keep: %v", err)
		}
	}
	mustRun(t, seed, "git", "add", "-A")
	mustRun(t, seed, "git", "commit", "-m", "seed")
	mustRun(t, seed, "git", "remote", "add", "origin", bare)
	mustRun(t, seed, "git", "push", "origin", "master")
	return bare
}

// mustRun runs cmd in dir and fatals on error, with combined output in
// the failure message so a test can show the underlying git complaint.
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// runCapture runs cmd in dir and returns trimmed stdout, fatal-ing on
// error. Used for assertions on git state.
func runCapture(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestEnsureCloneCreatesCacheDirOnFirstCall(t *testing.T) {
	bare := initBareRemote(t, map[string]string{
		"Home.md": "# Home\nbody",
	})
	cache := t.TempDir()

	c := &wikiCache{root: cache, ttl: time.Minute}
	dir, err := c.ensureClone(context.Background(), "owner", "repo", bare)
	if err != nil {
		t.Fatalf("ensureClone: %v", err)
	}
	if !strings.HasPrefix(dir, cache) {
		t.Errorf("clone path %q should be under cache root %q", dir, cache)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("clone should contain .git: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Home.md")); err != nil {
		t.Errorf("clone should contain seeded Home.md: %v", err)
	}

	info, err := os.Stat(filepath.Dir(dir))
	if err != nil {
		t.Fatalf("stat owner dir: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("owner dir mode = %o, want 0700", mode)
	}
}

func TestEnsureCloneSecondCallSkipsClone(t *testing.T) {
	bare := initBareRemote(t, map[string]string{"Home.md": "first"})
	cache := t.TempDir()
	c := &wikiCache{root: cache, ttl: time.Hour}

	dir, err := c.ensureClone(context.Background(), "o", "r", bare)
	if err != nil {
		t.Fatalf("first ensureClone: %v", err)
	}
	// Modify the local file so we can prove a no-op refresh leaves it.
	marker := filepath.Join(dir, "local-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	// Push a new commit upstream that we should NOT see locally if the
	// TTL hasn't expired.
	pushFreshCommit(t, bare, "Home.md", "updated")

	dir2, err := c.ensureClone(context.Background(), "o", "r", bare)
	if err != nil {
		t.Fatalf("second ensureClone: %v", err)
	}
	if dir != dir2 {
		t.Errorf("paths should match: %q vs %q", dir, dir2)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker should still exist (no refresh expected): %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir2, "Home.md"))
	if err != nil {
		t.Fatalf("read Home.md: %v", err)
	}
	if string(body) != "first" {
		t.Errorf("body should still be original (TTL valid); got %q", body)
	}
}

func TestEnsureCloneRefreshesAfterTTL(t *testing.T) {
	bare := initBareRemote(t, map[string]string{"Home.md": "first"})
	cache := t.TempDir()
	// Zero TTL → every call refreshes.
	c := &wikiCache{root: cache, ttl: 0}

	if _, err := c.ensureClone(context.Background(), "o", "r", bare); err != nil {
		t.Fatalf("first ensureClone: %v", err)
	}
	pushFreshCommit(t, bare, "Home.md", "updated")

	dir, err := c.ensureClone(context.Background(), "o", "r", bare)
	if err != nil {
		t.Fatalf("second ensureClone: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "Home.md"))
	if err != nil {
		t.Fatalf("read Home.md: %v", err)
	}
	if string(body) != "updated" {
		t.Errorf("body should reflect refresh; got %q", body)
	}
}

func TestCommitAndPushUploadsChange(t *testing.T) {
	bare := initBareRemote(t, map[string]string{"Home.md": "first"})
	cache := t.TempDir()
	c := &wikiCache{root: cache, ttl: time.Hour}

	dir, err := c.ensureClone(context.Background(), "o", "r", bare)
	if err != nil {
		t.Fatalf("ensureClone: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Home.md"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write Home.md: %v", err)
	}
	if err := c.commitAndPush(context.Background(), dir, "edit Home"); err != nil {
		t.Fatalf("commitAndPush: %v", err)
	}

	// Verify the bare repo received the new commit.
	logOut := runCapture(t, bare, "git", "log", "--format=%s", "master")
	if !strings.Contains(logOut, "edit Home") {
		t.Errorf("bare repo log should contain pushed commit; got %q", logOut)
	}
}

func TestCommitAndPushFailsHardOnPushError(t *testing.T) {
	bare := initBareRemote(t, map[string]string{"Home.md": "first"})
	cache := t.TempDir()
	c := &wikiCache{root: cache, ttl: time.Hour}

	dir, err := c.ensureClone(context.Background(), "o", "r", bare)
	if err != nil {
		t.Fatalf("ensureClone: %v", err)
	}
	// Detach the remote so push has nowhere to go.
	if err := os.RemoveAll(bare); err != nil {
		t.Fatalf("rm bare: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Home.md"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write Home.md: %v", err)
	}
	err = c.commitAndPush(context.Background(), dir, "edit Home")
	if err == nil {
		t.Fatal("expected push to fail")
	}
}

func TestNewWikiCacheCreatesRoot(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	c, err := newWikiCache("token123")
	if err != nil {
		t.Fatalf("newWikiCache: %v", err)
	}
	want := filepath.Join(xdg, "gaia", "wikis")
	if c.root != want {
		t.Errorf("root: got %q, want %q", c.root, want)
	}
	if c.ttl != defaultWikiCacheTTL {
		t.Errorf("ttl: got %v, want %v", c.ttl, defaultWikiCacheTTL)
	}
	if c.token != "token123" {
		t.Errorf("token not propagated")
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("root not created: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("root mode = %o, want 0700", mode)
	}
}

func TestDefaultWikiCacheRootRespectsXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdg)
	got, err := defaultWikiCacheRoot()
	if err != nil {
		t.Fatalf("defaultWikiCacheRoot: %v", err)
	}
	if got != filepath.Join(xdg, "gaia", "wikis") {
		t.Errorf("got %q", got)
	}
}

func TestDefaultWikiCacheRootFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	got, err := defaultWikiCacheRoot()
	if err != nil {
		t.Fatalf("defaultWikiCacheRoot: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".cache", "gaia", "wikis")) {
		t.Errorf("got %q, expected suffix .cache/gaia/wikis", got)
	}
}

func TestWikiRemoteURLEmbedsTokenWhenPresent(t *testing.T) {
	got := wikiRemoteURL("", "alice", "myrepo", "secrettoken")
	want := "https://x-access-token:secrettoken@github.com/alice/myrepo.wiki.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWikiRemoteURLOmitsAuthWhenTokenEmpty(t *testing.T) {
	got := wikiRemoteURL("", "alice", "myrepo", "")
	want := "https://github.com/alice/myrepo.wiki.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWikiRemoteURLAllowsCustomHost(t *testing.T) {
	got := wikiRemoteURL("ghe.example.com", "o", "r", "t")
	if !strings.HasPrefix(got, "https://x-access-token:t@ghe.example.com/") {
		t.Errorf("got %q", got)
	}
}

// Path-segment validation regression tests (#136).
//
// owner / repo / slug each come from caller-controlled input (CLI
// flag or MCP client) and are joined into a filesystem path inside
// the cache root. validatePathSegment is the boundary check: it
// rejects empty strings, `.` / `..`, anything containing a path
// separator, hidden-file prefixes, and anything that doesn't match
// the strict allowlist. Tests below exercise each rejected shape +
// confirm valid names still pass.

func TestValidatePathSegmentRejectsTraversal(t *testing.T) {
	cases := []string{
		"",
		".",
		"..",
		"../etc",
		"foo/bar",
		"foo\\bar",
		".hidden",
		"name\x00with-null",
		"name with space",
		"a$b",
		"a*b",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if err := validatePathSegment(in); err == nil {
				t.Errorf("validatePathSegment(%q) = nil; want error", in)
			}
		})
	}
}

func TestValidatePathSegmentAcceptsSafeNames(t *testing.T) {
	cases := []string{
		"alice",
		"My-Repo",
		"my_repo.0",
		"Home",
		"a",
		"A1B2C3",
		"Setup-Guide",
		"page.with.dots",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if err := validatePathSegment(in); err != nil {
				t.Errorf("validatePathSegment(%q) = %v; want nil", in, err)
			}
		})
	}
}

func TestEnsureCloneRejectsTraversalOwner(t *testing.T) {
	cache := t.TempDir()
	c := &wikiCache{root: cache, ttl: time.Minute}
	_, err := c.ensureClone(context.Background(), "../../../etc", "repo", "ignored")
	if err == nil {
		t.Fatal("expected error on traversal owner")
	}
	// Defence-in-depth: also assert nothing was created outside the
	// cache root. Walk the cache root's PARENT and ensure the only
	// thing inside is `cache` itself (the t.TempDir we configured).
	parent := filepath.Dir(cache)
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		full := filepath.Join(parent, e.Name())
		if full == cache {
			continue
		}
		// `etc` would only exist if the owner segment escaped.
		if e.Name() == "etc" || strings.HasPrefix(e.Name(), "..") {
			t.Errorf("traversal escaped: created %s", full)
		}
	}
}

func TestEnsureCloneRejectsTraversalRepo(t *testing.T) {
	cache := t.TempDir()
	c := &wikiCache{root: cache, ttl: time.Minute}
	_, err := c.ensureClone(context.Background(), "alice", ".git", "ignored")
	if err == nil {
		t.Fatal("expected error on suspicious repo segment")
	}
}

func TestEnsureCloneRejectsTraversalRepoDotDot(t *testing.T) {
	cache := t.TempDir()
	c := &wikiCache{root: cache, ttl: time.Minute}
	_, err := c.ensureClone(context.Background(), "alice", "..", "ignored")
	if err == nil {
		t.Fatal("expected error on `..` repo segment")
	}
}

// pushFreshCommit clones the bare repo to a temp dir, replaces a file's
// content, commits, and pushes back. Helper used by tests that want
// upstream to advance between cache calls. Importantly, the helper
// pushes a *forward* commit on top of the existing tip rather than
// replacing history — gaia's cache uses `--ff-only` and would reject a
// rewritten upstream.
func pushFreshCommit(t *testing.T, bare, file, content string) {
	t.Helper()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "work")
	// Full clone (no --depth) so the helper has the parent commit
	// objects locally and can push a strict fast-forward back.
	mustRun(t, tmp, "git", "clone", bare, "work")
	mustRun(t, work, "git", "config", "user.email", "test@example")
	mustRun(t, work, "git", "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(work, file), []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustRun(t, work, "git", "add", "-A")
	mustRun(t, work, "git", "commit", "-m", "fresh")
	mustRun(t, work, "git", "push", "origin", "master")
}
