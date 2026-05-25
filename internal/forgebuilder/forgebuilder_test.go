package forgebuilder_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/settings"
	"github.com/stewartbrothers/gaia/internal/forgebuilder"
)

// loadSettings is a per-test helper that constructs a Settings with
// the override the migrated tests previously passed to Build directly.
// settings.Load resolves config + credentials + env eagerly, so the
// test exercises the same path Build now reads from.
func loadSettings(t *testing.T, ov settings.Override) settings.Settings {
	t.Helper()
	s, err := settings.Load(ov)
	if err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	return s
}

// TestBuildSkipsCacheWhenEnvDisablesIt pins the #303 fix: setting
// GAIA_CACHE_ENABLED=false short-circuits the sqlite open in Build,
// so no cache file lands in $XDG_CACHE_HOME/gaia. Without this
// path, every CLI test that calls cli.NewRootCmd()+Execute() opens
// (and leaks) a real on-disk sqlite DB — pushing CI past the
// per-package 10-minute test timeout on Linux runners.
//
// Test strategy: point HOME + XDG_CACHE_HOME at fresh tempdirs, set
// the env, build a forgejo provider, then assert the cache dir is
// empty. A second pass with the env unset (default) confirms the
// cache *would* have been created absent the override — guards
// against the assertion silently passing because PathFor changed.
func TestBuildSkipsCacheWhenEnvDisablesIt(t *testing.T) {
	cacheDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("FORGEJO_TOKEN", "X")
	// Clear any inherited setting; this test owns it.
	t.Setenv("GAIA_CACHE_ENABLED", "false")

	s := loadSettings(t, settings.Override{
		Provider: "forgejo",
		APIURL:   "https://example.test/api/v1",
	})

	_, _, err := forgebuilder.Build(s, forgebuilder.BuildOverride{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(cacheDir, "gaia"))
	if err == nil && len(entries) > 0 {
		t.Errorf("GAIA_CACHE_ENABLED=false should suppress sqlite open; found %d cache entries under %s",
			len(entries), filepath.Join(cacheDir, "gaia"))
	}
	// IsNotExist is the happy path: forgebuilder never touched the dir.
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("ReadDir: %v", err)
	}
}

// TestBuildOpensCacheByDefault confirms the suppression test above
// would catch a regression: without the env var, Build *does* open
// a cache file under $XDG_CACHE_HOME/gaia. Pinning both halves
// prevents the no-op-on-both case where PathFor silently changed.
func TestBuildOpensCacheByDefault(t *testing.T) {
	cacheDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", cacheDir)
	t.Setenv("FORGEJO_TOKEN", "X")
	t.Setenv("GAIA_CACHE_ENABLED", "") // default = enabled

	s := loadSettings(t, settings.Override{
		Provider: "forgejo",
		APIURL:   "https://example.test/api/v1",
	})

	_, _, err := forgebuilder.Build(s, forgebuilder.BuildOverride{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(cacheDir, "gaia"))
	if err != nil {
		t.Fatalf("expected cache dir to exist by default: %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("expected at least one cache entry under %s; got 0", filepath.Join(cacheDir, "gaia"))
	}
}
