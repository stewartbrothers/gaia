package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/github"
)

// githubUserJSON returns a minimal GitHub user/org record.
func githubUserJSON(login, userType string) map[string]any {
	return map[string]any{
		"login": login,
		"type":  userType,
	}
}

// githubPackageVersionJSON returns a minimal GitHub package version record.
func githubPackageVersionJSON(id int64, name string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"created_at": "2026-04-01T00:00:00Z",
		"metadata":   map[string]any{},
	}
}

// TestGitHubGetPackageFreshHitSkipsUpstream: a fresh cache entry should
// be returned without any upstream calls at all (#153).
func TestGitHubGetPackageFreshHitSkipsUpstream(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := openCache(t)
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

	// Repo="" for owner-scoped packages; ID = "type/name/version"
	cachedPayload, _ := json.Marshal(map[string]any{
		"type":    "npm",
		"name":    "mylib",
		"version": "1.0.0",
		"owner":   "o",
	})
	key := cache.Key{Kind: "package", Owner: "o", Repo: "", ID: "npm/mylib/1.0.0"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: cachedPayload,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := prov.GetPackage(context.Background(), "o", "npm", "mylib", "1.0.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Name != "mylib" {
		t.Errorf("expected cached name=mylib; got %q", got.Name)
	}
	if called {
		t.Error("upstream should not be hit on fresh cache")
	}
}

// TestGitHubGetPackageCacheMissGoesUpstream: no cache entry → upstream
// is called (user type probe + version list + version fetch) and the
// result is stored in the cache (#153).
func TestGitHubGetPackageCacheMissGoesUpstream(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(githubUserJSON("o", "User"))
		case "/users/o/packages/npm/betapack/versions":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				githubPackageVersionJSON(11, "2.0.0"),
			})
		case "/users/o/packages/npm/betapack/versions/11":
			_ = json.NewEncoder(w).Encode(githubPackageVersionJSON(11, "2.0.0"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := openCache(t)
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

	got, err := prov.GetPackage(context.Background(), "o", "npm", "betapack", "2.0.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Name != "betapack" {
		t.Errorf("expected name=betapack; got %q", got.Name)
	}

	// After the fetch, the cache should contain the entry.
	key := cache.Key{Kind: "package", Owner: "o", Repo: "", ID: "npm/betapack/2.0.0"}
	if _, ok, _ := c.Lookup(context.Background(), key); !ok {
		t.Error("expected cache entry after upstream fetch")
	}
}

// TestGitHubDeletePackageEvictsCachedRow: DeletePackage must remove the
// cached object row so the next GetPackage re-fetches (#153).
func TestGitHubDeletePackageEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/o":
			_ = json.NewEncoder(w).Encode(githubUserJSON("o", "User"))
		case "/users/o/packages/npm/mylib/versions":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				githubPackageVersionJSON(5, "1.0.0"),
			})
		default:
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

	key := cache.Key{Kind: "package", Owner: "o", Repo: "", ID: "npm/mylib/1.0.0"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"type":"npm","name":"mylib","version":"1.0.0"}`),
	})

	if err := prov.DeletePackage(context.Background(), "o", "npm", "mylib", "1.0.0"); err != nil {
		t.Fatalf("DeletePackage: %v", err)
	}

	if _, ok, _ := c.Lookup(context.Background(), key); ok {
		t.Error("DeletePackage should evict its cache row")
	}
}
