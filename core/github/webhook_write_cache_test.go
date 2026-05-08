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
	"github.com/stewartbrothers/gaia/core/provider"
)

// githubHookJSON returns a GitHub-shaped webhook record for httptest.
func githubHookJSON(id int64, hookURL, ct string, events []string, active bool) map[string]any {
	return map[string]any{
		"id":   id,
		"name": "web",
		"config": map[string]any{
			"url":          hookURL,
			"content_type": ct,
		},
		"events":     events,
		"active":     active,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-01T01:00:00Z",
	}
}

// TestGitHubCreateWebhookFlushesListCache: CreateWebhook must invalidate
// the webhook-list cache for the repo (#153).
func TestGitHubCreateWebhookFlushesListCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(githubHookJSON(99, "https://example.com/new", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

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

// TestGitHubGetWebhookFreshHitSkipsUpstream: a fresh cache entry should
// be returned without touching upstream at all (#153).
func TestGitHubGetWebhookFreshHitSkipsUpstream(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := openCache(t)
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

	cachedPayload, _ := json.Marshal(githubHookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
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

// TestGitHubEditWebhookEvictsCachedRow: EditWebhook must remove the
// object row so the next GetWebhook re-fetches from upstream (#153).
func TestGitHubEditWebhookEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(githubHookJSON(42, "https://example.com/x", "json", []string{"push"}, true))
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

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

// TestGitHubDeleteWebhookEvictsCachedRow: DeleteWebhook must remove
// the object row for the deleted hook (#153).
func TestGitHubDeleteWebhookEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := cache.NewMemory()
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

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

// TestGitHubGetWebhookDeliveryFreshHitSkipsUpstream: a fresh delivery
// cache entry should be returned without touching upstream (#153).
func TestGitHubGetWebhookDeliveryFreshHitSkipsUpstream(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := openCache(t)
	prov := github.NewProvider(github.Options{BaseURL: srv.URL, Token: "X", Cache: c})

	cachedPayload, _ := json.Marshal(map[string]any{
		"id":           int64(77),
		"event":        "push",
		"action":       "",
		"status_code":  200,
		"duration":     0.5,
		"redelivery":   false,
		"delivered_at": "2026-04-01T00:00:00Z",
	})
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
