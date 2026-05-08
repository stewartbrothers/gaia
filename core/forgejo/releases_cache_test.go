package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

// TestGetReleaseFreshHitSkipsUpstream: a fresh cache entry should be
// returned without touching upstream at all (#153).
func TestGetReleaseFreshHitSkipsUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500) // would fail the test if reached
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	cachedPayload, _ := json.Marshal(releaseJSON(1, "v1.0.0", "Cached Release", false, false))
	key := cache.Key{Kind: "release", Owner: "o", Repo: "r", ID: "v1.0.0"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: cachedPayload,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := prov.GetRelease(context.Background(), "o", "r", "v1.0.0")
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got.TagName != "v1.0.0" {
		t.Errorf("expected cached tag name; got %q", got.TagName)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream should not be hit on fresh cache; got hits=%d", h)
	}
}

// TestGetReleaseCacheMissGoesUpstream: no cache entry → upstream is
// called and the result is stored in the cache (#153).
func TestGetReleaseCacheMissGoesUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `"etag-rel"`)
		_ = json.NewEncoder(w).Encode(releaseJSON(42, "v2.0.0", "New Release", false, false))
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	got, err := prov.GetRelease(context.Background(), "o", "r", "v2.0.0")
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got.TagName != "v2.0.0" {
		t.Errorf("expected upstream tag; got %q", got.TagName)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected one upstream hit; got %d", h)
	}

	// Cache should now hold a row for this key.
	key := cache.Key{Kind: "release", Owner: "o", Repo: "r", ID: "v2.0.0"}
	if _, ok, _ := c.Lookup(context.Background(), key); !ok {
		t.Error("expected cache entry after upstream fetch")
	}
}
