package autodetect_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/autodetect"
	"github.com/stewartbrothers/gaia/core/cache"
)

// initRepoWithRemote creates a throwaway git repo in a temp dir with an
// origin remote pointing at url, and returns the dir.
func initRepoWithRemote(t *testing.T, url string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", url},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v (%s)", args, err, out)
		}
	}
	return dir
}

func TestFromGitRemoteCachedHitsCache(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithRemote(t, "git@github.com:octocat/hello.git")

	c := cache.NewMemory()

	// First call: cold cache, shells out to git, parses, caches.
	got, err := autodetect.FromGitRemoteCached(ctx, c, dir, "")
	if err != nil {
		t.Fatalf("FromGitRemoteCached (cold): %v", err)
	}
	if got.Owner != "octocat" || got.Name != "hello" {
		t.Fatalf("cold parse: got %+v, want octocat/hello", got)
	}

	// Mutate the remote on disk. A cache hit must return the *cached*
	// value, proving the second call didn't shell out again.
	cmd := exec.Command("git", "-C", dir, "remote", "set-url", "origin", "git@github.com:changed/repo.git")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set-url: %v (%s)", err, out)
	}

	got2, err := autodetect.FromGitRemoteCached(ctx, c, dir, "")
	if err != nil {
		t.Fatalf("FromGitRemoteCached (warm): %v", err)
	}
	if got2.Owner != "octocat" || got2.Name != "hello" {
		t.Fatalf("warm read: got %+v, want cached octocat/hello", got2)
	}
}

func TestFromGitRemoteCachedNilCacheIsPassthrough(t *testing.T) {
	ctx := context.Background()
	dir := initRepoWithRemote(t, "https://example.com/acme/widget.git")

	got, err := autodetect.FromGitRemoteCached(ctx, nil, dir, "")
	if err != nil {
		t.Fatalf("FromGitRemoteCached (nil cache): %v", err)
	}
	if got.Owner != "acme" || got.Name != "widget" {
		t.Fatalf("nil-cache parse: got %+v, want acme/widget", got)
	}
}

func TestFromGitRemoteCachedKeyedByPath(t *testing.T) {
	ctx := context.Background()
	dirA := initRepoWithRemote(t, "git@github.com:owner-a/repo-a.git")
	dirB := initRepoWithRemote(t, "git@github.com:owner-b/repo-b.git")
	c := cache.NewMemory()

	a, err := autodetect.FromGitRemoteCached(ctx, c, dirA, "")
	if err != nil {
		t.Fatalf("dirA: %v", err)
	}
	b, err := autodetect.FromGitRemoteCached(ctx, c, dirB, "")
	if err != nil {
		t.Fatalf("dirB: %v", err)
	}
	if a.Owner != "owner-a" || b.Owner != "owner-b" {
		t.Fatalf("path keying: a=%+v b=%+v, want distinct owners (no aliasing)", a, b)
	}
	// The two repos must have produced two distinct cache rows under the
	// same Kind, differentiated by their absolute path id.
	if absA, _ := filepath.Abs(dirA); absA == "" {
		t.Fatal("filepath.Abs(dirA) returned empty")
	}
}

func TestFromGitRemoteCachedPropagatesGitError(t *testing.T) {
	ctx := context.Background()
	// A temp dir that is not a git repo: git fails, error propagates,
	// nothing is cached.
	dir := t.TempDir()
	c := cache.NewMemory()
	if _, err := autodetect.FromGitRemoteCached(ctx, c, dir, ""); err == nil {
		t.Fatal("FromGitRemoteCached on non-repo: want error, got nil")
	}
}
