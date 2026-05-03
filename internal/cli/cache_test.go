package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// TestCacheNukeRemovesAllCacheFiles: with two prepopulated cache
// files, `gaia cache nuke` removes them both and reports what was
// cleaned to the user.
func TestCacheNukeRemovesAllCacheFiles(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheRoot))
	// XDG_CACHE_HOME → <root>/gaia
	gaiaCache := filepath.Join(filepath.Dir(cacheRoot), "gaia")
	if err := os.MkdirAll(gaiaCache, 0o700); err != nil {
		t.Fatal(err)
	}

	dbA := filepath.Join(gaiaCache, "forgejo", "host-a.db")
	dbB := filepath.Join(gaiaCache, "github", "api.github.com.db")
	for _, p := range []string{dbA, dbB} {
		c, err := cache.Open(p)
		if err != nil {
			t.Fatalf("seed cache %s: %v", p, err)
		}
		_ = c.Store(context.Background(), cache.Entry{
			Key:       cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"},
			FetchedAt: time.Now(), TTL: time.Minute, Payload: []byte(`{}`),
		})
		_ = c.Close()
	}

	root := cli.NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"cache", "nuke"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cache nuke: %v\nstderr: %s", err, stderr.String())
	}

	for _, p := range []string{dbA, dbB} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed; stat err=%v", p, err)
		}
	}
	if !strings.Contains(stdout.String(), "removed") {
		t.Errorf("expected 'removed' in output, got %q", stdout.String())
	}
}

// TestCacheNukeProviderFilter: --provider=forgejo only nukes forgejo
// caches, leaving github intact.
func TestCacheNukeProviderFilter(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", filepath.Dir(cacheRoot))
	gaiaCache := filepath.Join(filepath.Dir(cacheRoot), "gaia")
	if err := os.MkdirAll(gaiaCache, 0o700); err != nil {
		t.Fatal(err)
	}
	dbF := filepath.Join(gaiaCache, "forgejo", "host-a.db")
	dbG := filepath.Join(gaiaCache, "github", "api.github.com.db")
	for _, p := range []string{dbF, dbG} {
		c, _ := cache.Open(p)
		_ = c.Store(context.Background(), cache.Entry{
			Key:       cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"},
			FetchedAt: time.Now(), TTL: time.Minute, Payload: []byte(`{}`),
		})
		_ = c.Close()
	}

	root := cli.NewRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"cache", "nuke", "--provider", "forgejo"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cache nuke: %v\noutput: %s", err, stdout.String())
	}

	if _, err := os.Stat(dbF); !os.IsNotExist(err) {
		t.Errorf("forgejo cache should be gone; stat err=%v", err)
	}
	if _, err := os.Stat(dbG); err != nil {
		t.Errorf("github cache should NOT be touched; stat err=%v", err)
	}
}

// TestCacheNukeNoCachePresentIsClean: no cache dir present is not an
// error — the command exits 0 and reports nothing to do.
func TestCacheNukeWithNoCacheDirIsCleanExit(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := cli.NewRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"cache", "nuke"})
	if err := root.Execute(); err != nil {
		t.Fatalf("cache nuke (empty dir): %v", err)
	}
	if !strings.Contains(stdout.String(), "no cache") && !strings.Contains(stdout.String(), "nothing") {
		t.Errorf("expected 'no cache' or 'nothing' in output; got %q", stdout.String())
	}
}

// TestNoCacheGlobalFlagAccepted: --no-cache should be a recognized
// persistent flag — its semantic effect (cache bypass) is checked
// separately in core/forgejo, but the CLI must at least accept it
// without "unknown flag" errors.
func TestNoCacheGlobalFlagAccepted(t *testing.T) {
	root := cli.NewRootCmd()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"--no-cache", "version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--no-cache version: %v\noutput: %s", err, stdout.String())
	}
}
