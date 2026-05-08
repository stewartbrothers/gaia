package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

// TestCreateWebhookFlushesListCache: CreateWebhook must invalidate
// the webhook-list cache for the repo so the next list call re-fetches.
func TestCreateWebhookFlushesListCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(hookJSON(99, "https://example.com/new", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Pre-populate a list cache row.
	listKey := cache.ListKey{Kind: "webhook", Owner: "o", Repo: "r", QueryHash: "abc"}
	_ = c.StoreList(context.Background(), cache.ListEntry{
		Key: listKey, FetchedAt: time.Now(), TTL: 30 * time.Second,
		Payload: []byte(`["1","2"]`),
	})

	if _, err := prov.CreateWebhook(context.Background(), "o", "r", provider.CreateWebhookOptions{
		URL:    "https://example.com/new",
		Events: []string{"push"},
	}); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	if _, ok, _ := c.LookupList(context.Background(), listKey); ok {
		t.Error("CreateWebhook should flush webhook-list cache for the repo")
	}
}

// TestGetWebhookFreshHitSkipsUpstream: a fresh cache entry should be
// returned without touching upstream at all (#153).
func TestGetWebhookFreshHitSkipsUpstream(t *testing.T) {
	var hits int32
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_ = hits // avoid unused variable
		w.WriteHeader(500)
	}))
	defer srv.Close()
	_ = called

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	cachedPayload, _ := json.Marshal(hookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	key := cache.Key{Kind: "webhook", Owner: "o", Repo: "r", ID: "42"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: cachedPayload,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := prov.GetWebhook(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.ID != 42 {
		t.Errorf("expected cached ID=42; got %d", got.ID)
	}
	if called {
		t.Error("upstream should not be hit on fresh cache")
	}
}

// TestEditWebhookEvictsCachedRow: EditWebhook must remove the object
// row so the next GetWebhook re-fetches from upstream.
func TestEditWebhookEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle both GET (for event merge) and PATCH.
		_ = json.NewEncoder(w).Encode(hookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Pre-populate the object cache row.
	key := cache.Key{Kind: "webhook", Owner: "o", Repo: "r", ID: "42"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"id":42,"url":"https://example.com/old"}`),
	})

	if _, err := prov.EditWebhook(context.Background(), "o", "r", 42, provider.EditWebhookOptions{}); err != nil {
		t.Fatalf("EditWebhook: %v", err)
	}

	if _, ok, _ := c.Lookup(context.Background(), key); ok {
		t.Error("EditWebhook should evict its cache row")
	}
}

// TestDeleteWebhookEvictsCachedRow: DeleteWebhook must remove the
// object row for the deleted hook.
func TestDeleteWebhookEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	key := cache.Key{Kind: "webhook", Owner: "o", Repo: "r", ID: "42"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"id":42}`),
	})

	if err := prov.DeleteWebhook(context.Background(), "o", "r", 42); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}

	if _, ok, _ := c.Lookup(context.Background(), key); ok {
		t.Error("DeleteWebhook should evict its cache row")
	}
}

// TestGetWebhookDeliveryFreshHitSkipsUpstream: a fresh delivery cache
// entry should be returned without touching upstream (#153).
func TestGetWebhookDeliveryFreshHitSkipsUpstream(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := openCache(t)
	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	cachedPayload, _ := json.Marshal(map[string]any{
		"id":              int64(77),
		"event":           "push",
		"action":          "",
		"response_status": 200,
		"duration":        0.5,
		"is_redelivery":   false,
		"delivered_at":    "2026-04-01T00:00:00Z",
	})
	// key shape: kindDelivery, hookID/deliveryID
	key := cache.Key{Kind: "delivery", Owner: "o", Repo: "r", ID: "42/77"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: cachedPayload,
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	got, err := prov.GetWebhookDelivery(context.Background(), "o", "r", 42, 77)
	if err != nil {
		t.Fatalf("GetWebhookDelivery: %v", err)
	}
	if got.ID != 77 {
		t.Errorf("expected cached ID=77; got %d", got.ID)
	}
	if called {
		t.Error("upstream should not be hit on fresh cache")
	}
}
