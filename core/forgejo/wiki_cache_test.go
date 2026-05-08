package forgejo_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

// TestGetWikiPageFreshHitSkipsUpstream: a fresh cache entry should be
// returned without touching upstream at all (#153).
func TestGetWikiPageFreshHitSkipsUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500) // would fail the test if reached
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Seed a fresh cache entry for slug "Home".
	cachedPayload, _ := json.Marshal(map[string]any{
		"title":          "Home",
		"sub_url":        "Home",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("cached body")),
	})
	key := cache.Key{Kind: "wiki", Owner: "o", Repo: "r", ID: "Home"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: cachedPayload,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := prov.GetWikiPage(context.Background(), "o", "r", "Home")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Body != "cached body" {
		t.Errorf("expected body from cache; got %q", got.Body)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream should not be hit on fresh cache; got hits=%d", h)
	}
}

// TestGetWikiPageCacheMissGoesUpstream: no cache entry → upstream is
// called and the result is stored in the cache (#153).
func TestGetWikiPageCacheMissGoesUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `"etag1"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":          "Home",
			"sub_url":        "Home",
			"content_base64": base64.StdEncoding.EncodeToString([]byte("upstream body")),
		})
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	got, err := prov.GetWikiPage(context.Background(), "o", "r", "Home")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Body != "upstream body" {
		t.Errorf("expected upstream body; got %q", got.Body)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected one upstream hit; got %d", h)
	}

	// Cache should now hold a row for this key.
	key := cache.Key{Kind: "wiki", Owner: "o", Repo: "r", ID: "Home"}
	if _, ok, _ := c.Lookup(context.Background(), key); !ok {
		t.Error("expected cache entry after upstream fetch")
	}
}
