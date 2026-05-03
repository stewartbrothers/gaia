package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/github"
)

func openCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "gh-cache.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestGitHubGetCachedFreshHitSkipsRequest(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()
	c := openCache(t)
	cl := github.New(github.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})
	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"who":"cache"}`),
	}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, key, time.Minute); err != nil {
		t.Fatalf("GetCached: %v", err)
	}
	if got["who"] != "cache" {
		t.Errorf("got %v", got)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("expected zero upstream hits, got %d", h)
	}
}

func TestGitHubGetCached304BumpsTimestamp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()
	c := openCache(t)
	cl := github.New(github.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})
	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, ETag: `"v1"`, FetchedAt: time.Now().Add(-2 * time.Hour),
		TTL: time.Minute, Payload: []byte(`{"who":"cache"}`),
	})
	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got["who"] != "cache" {
		t.Errorf("expected cached payload after 304; got %v", got)
	}
	entry, _, _ := c.Lookup(context.Background(), key)
	if time.Since(entry.FetchedAt) > 5*time.Second {
		t.Errorf("Touch should bump fetched_at; got %v", entry.FetchedAt)
	}
}

func TestGitHubGetCached200StoresFreshRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v2"`)
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()
	c := openCache(t)
	cl := github.New(github.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})
	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	entry, ok, _ := c.Lookup(context.Background(), key)
	if !ok {
		t.Fatal("expected row stored after 200")
	}
	if entry.ETag != `"v2"` {
		t.Errorf("ETag captured: got %q", entry.ETag)
	}
}

func TestGitHubGetCachedFallsBackWithoutCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()
	cl := github.New(github.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond})
	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got["who"] != "upstream" {
		t.Errorf("got %v", got)
	}
}
