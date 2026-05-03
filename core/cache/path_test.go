package cache_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/cache"
)

func TestDefaultDirHonorsXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
	dir, err := cache.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if dir != "/tmp/xdg-test/gaia" {
		t.Errorf("dir: got %q, want /tmp/xdg-test/gaia", dir)
	}
}

func TestDefaultDirFallsBackToHome(t *testing.T) {
	// HOME is always set on the runners; XDG must take a back seat
	// when explicitly empty.
	t.Setenv("XDG_CACHE_HOME", "")
	dir, err := cache.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".cache", "gaia")) {
		t.Errorf("expected ~/.cache/gaia suffix, got %q", dir)
	}
}

func TestPathForExtractsHostFromAPIURL(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/x")
	cases := []struct {
		provider, apiURL, want string
	}{
		{"forgejo", "https://your-forge.example.com/api/v1", "/tmp/x/gaia/forgejo/your-forge.example.com.db"},
		{"github", "https://api.github.com", "/tmp/x/gaia/github/api.github.com.db"},
		// Bare hostname fallback (used in tests / weird configs).
		{"forgejo", "git.example.org", "/tmp/x/gaia/forgejo/git.example.org.db"},
	}
	for _, tc := range cases {
		got, err := cache.PathFor(tc.provider, tc.apiURL)
		if err != nil {
			t.Fatalf("PathFor(%s, %s): %v", tc.provider, tc.apiURL, err)
		}
		if got != tc.want {
			t.Errorf("PathFor(%s, %s): got %q want %q", tc.provider, tc.apiURL, got, tc.want)
		}
	}
}

func TestPathForRequiresProvider(t *testing.T) {
	if _, err := cache.PathFor("", "https://x"); err == nil {
		t.Error("expected error when provider is empty")
	}
}

func TestPathForRejectsEmptyHost(t *testing.T) {
	if _, err := cache.PathFor("forgejo", ""); err == nil {
		t.Error("expected error when api_url is empty")
	}
}

func TestPathForSanitizesPathSeparatorsInHost(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/x")
	// Pathological input: a malformed URL whose host contains separators.
	// PathFor must NOT allow ".." or "/" to escape the cache dir.
	got, err := cache.PathFor("forgejo", "ev/il/../host")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	if strings.Contains(got, "..") {
		t.Errorf("traversal token survived sanitization: %q", got)
	}
	if !strings.HasPrefix(got, "/tmp/x/gaia/forgejo/") {
		t.Errorf("escaped expected prefix: %q", got)
	}
}

func TestHashQueryStableUnderKeyOrdering(t *testing.T) {
	a := cache.HashQuery(map[string]any{"state": "open", "labels": "bug,urgent"})
	b := cache.HashQuery(map[string]any{"labels": "bug,urgent", "state": "open"})
	if a != b {
		t.Errorf("HashQuery should be stable under map ordering: %q vs %q", a, b)
	}
}

func TestHashQueryDistinctForDifferentParams(t *testing.T) {
	a := cache.HashQuery(map[string]any{"state": "open"})
	b := cache.HashQuery(map[string]any{"state": "closed"})
	if a == b {
		t.Error("HashQuery should differ when value differs")
	}
}

func TestHashQueryEmptyAndNilAreEquivalent(t *testing.T) {
	a := cache.HashQuery(nil)
	b := cache.HashQuery(map[string]any{})
	if a != b {
		t.Errorf("empty + nil should hash identically; got %q vs %q", a, b)
	}
	if a == "" {
		t.Error("HashQuery(nil) should still return a digest, not empty")
	}
}
