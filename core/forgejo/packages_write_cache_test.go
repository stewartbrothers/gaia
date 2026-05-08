package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

// TestGetPackageFreshHitSkipsUpstream: a fresh cache entry should be
// returned without touching upstream at all (#153).
func TestGetPackageFreshHitSkipsUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Repo="" for owner-scoped packages; ID = "type/name/version"
	cachedPayload, _ := json.Marshal(packageJSON("generic", "alpha", "1.0.0", "o"))
	key := cache.Key{Kind: "package", Owner: "o", Repo: "", ID: "generic/alpha/1.0.0"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: cachedPayload,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := prov.GetPackage(context.Background(), "o", "generic", "alpha", "1.0.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Name != "alpha" {
		t.Errorf("expected cached name=alpha; got %q", got.Name)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream should not be hit on fresh cache; got hits=%d", h)
	}
}

// TestGetPackageCacheMissGoesUpstream: no cache entry → upstream is
// called and the result is stored in the cache (#153).
func TestGetPackageCacheMissGoesUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `"etag-pkg"`)
		_ = json.NewEncoder(w).Encode(packageJSON("generic", "beta", "2.0.0", "o"))
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	got, err := prov.GetPackage(context.Background(), "o", "generic", "beta", "2.0.0")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got.Name != "beta" {
		t.Errorf("expected upstream name=beta; got %q", got.Name)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected one upstream hit; got %d", h)
	}

	key := cache.Key{Kind: "package", Owner: "o", Repo: "", ID: "generic/beta/2.0.0"}
	if _, ok, _ := c.Lookup(context.Background(), key); !ok {
		t.Error("expected cache entry after upstream fetch")
	}
}

// TestDeletePackageEvictsCachedRow: DeletePackage must remove the
// cached object row so the next GetPackage re-fetches.
func TestDeletePackageEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	key := cache.Key{Kind: "package", Owner: "o", Repo: "", ID: "generic/alpha/1.0.0"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"type":"generic","name":"alpha","version":"1.0.0"}`),
	})

	if err := prov.DeletePackage(context.Background(), "o", "generic", "alpha", "1.0.0"); err != nil {
		t.Fatalf("DeletePackage: %v", err)
	}

	if _, ok, _ := c.Lookup(context.Background(), key); ok {
		t.Error("DeletePackage should evict its cache row")
	}
}

// TestUploadPackageFlushesListCache: UploadPackage (generic) must
// invalidate the package-list cache for the owner so the next list
// call re-fetches.
func TestUploadPackageFlushesListCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	listKey := cache.ListKey{Kind: "package", Owner: "o", Repo: "", QueryHash: "abc"}
	_ = c.StoreList(context.Background(), cache.ListEntry{
		Key: listKey, FetchedAt: time.Now(), TTL: 30 * time.Second,
		Payload: []byte(`["1","2"]`),
	})

	body := strings.NewReader("artifact bytes")
	if err := prov.UploadPackage(context.Background(), "o", "generic", "mypkg", "1.0.0",
		provider.UploadPackageOptions{FileName: "mypkg-1.0.0.tar.gz"}, body); err != nil {
		t.Fatalf("UploadPackage: %v", err)
	}

	if _, ok, _ := c.LookupList(context.Background(), listKey); ok {
		t.Error("UploadPackage should flush package-list cache for the owner")
	}
}
