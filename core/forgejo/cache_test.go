package forgejo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

// openCache builds a per-test cache (one DB file under t.TempDir).
func openCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "client-cache.db"))
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestClientGetCachedFreshHitSkipsRequest: a fresh cache hit means
// zero upstream requests — the trimmed payload comes straight from
// SQLite.
func TestClientGetCachedFreshHitSkipsRequest(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()

	c := openCache(t)
	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Pre-populate: a recent entry.
	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"who":"cache"}`),
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	var got map[string]string
	if err := cl.GetCached(context.Background(), "/whatever", &got, key, time.Minute); err != nil {
		t.Fatalf("GetCached: %v", err)
	}
	if got["who"] != "cache" {
		t.Errorf("expected payload from cache, got %v", got)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream should not be hit on fresh cache; got hits=%d", h)
	}
}

// TestClientGetCached304TouchesAndReturnsCachedPayload: stale entry
// with an ETag → upstream returns 304 → cache row's fetched_at bumps,
// and the returned payload is the one already stored.
func TestClientGetCached304ReusesPayload(t *testing.T) {
	var requestETag string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestETag = r.Header.Get("If-None-Match")
		if requestETag == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()

	c := openCache(t)
	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	// Seed an *expired* entry — TTL elapsed → conditional GET will fire.
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, ETag: `"v1"`, FetchedAt: time.Now().Add(-2 * time.Hour), TTL: time.Minute,
		Payload: []byte(`{"who":"cache"}`),
	}); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, key, time.Minute); err != nil {
		t.Fatalf("GetCached: %v", err)
	}
	if requestETag != `"v1"` {
		t.Errorf("expected If-None-Match header to carry stored ETag; got %q", requestETag)
	}
	if got["who"] != "cache" {
		t.Errorf("expected cached payload after 304; got %v", got)
	}

	// fetched_at must have been bumped.
	entry, _, _ := c.Lookup(context.Background(), key)
	if time.Since(entry.FetchedAt) > 5*time.Second {
		t.Errorf("Touch should have bumped fetched_at; %v", entry.FetchedAt)
	}
}

// TestClientGetCached200ReplacesAndStores: a 200 from upstream replaces
// the stale row, captures the new ETag, and returns the fresh payload.
func TestClientGetCached200ReplacesRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v2"`)
		w.Header().Set("Last-Modified", "Fri, 01 Jan 2026 00:00:00 GMT")
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()

	c := openCache(t)
	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	// Seed an old row with ETag "v1".
	if err := c.Store(context.Background(), cache.Entry{
		Key: key, ETag: `"v1"`, FetchedAt: time.Now().Add(-2 * time.Hour), TTL: time.Minute,
		Payload: []byte(`{"who":"old"}`),
	}); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got["who"] != "upstream" {
		t.Errorf("got %v want upstream", got)
	}

	entry, ok, _ := c.Lookup(context.Background(), key)
	if !ok {
		t.Fatal("expected cache entry after 200")
	}
	if entry.ETag != `"v2"` {
		t.Errorf("ETag: got %q want \"v2\"", entry.ETag)
	}
	if entry.LastModified == "" {
		t.Errorf("Last-Modified should have been captured")
	}
}

// TestClientGetCachedNoCacheBypassesEverything: when Client.Cache is nil
// (or NoCache=true on the call), GetCached falls back to a plain GET.
func TestClientGetCachedFallsBackWhenCacheNil(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode(map[string]string{"who": "upstream"})
	}))
	defer srv.Close()

	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond})
	var got map[string]string
	if err := cl.GetCached(context.Background(), "/x", &got, cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got["who"] != "upstream" {
		t.Errorf("got %v", got)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected one upstream hit when cache is nil; got %d", h)
	}
}

// TestClientGetCachedConditionalGETHonorsLastModified: when the row
// has Last-Modified but no ETag, the request still carries the
// If-Modified-Since header.
func TestClientGetCachedSendsIfModifiedSince(t *testing.T) {
	var lm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lm = r.Header.Get("If-Modified-Since")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	c := openCache(t)
	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, LastModified: "Fri, 01 Jan 2026 00:00:00 GMT",
		FetchedAt: time.Now().Add(-2 * time.Hour), TTL: time.Minute,
		Payload: []byte(`{}`),
	})

	var out map[string]string
	if err := cl.GetCached(context.Background(), "/x", &out, key, time.Minute); err != nil {
		t.Fatalf("GetCached: %v", err)
	}
	if lm != "Fri, 01 Jan 2026 00:00:00 GMT" {
		t.Errorf("If-Modified-Since: got %q", lm)
	}
}

// TestClientGetCachedDoesNotPopulateOnError: 5xx after retry should not
// mistakenly write an empty payload into the cache.
func TestClientGetCachedDoesNotPollutOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := openCache(t)
	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "9"}
	var out map[string]string
	err := cl.GetCached(context.Background(), "/x", &out, key, time.Minute)
	if err == nil {
		t.Fatal("expected error from 5xx upstream")
	}
	if _, ok, _ := c.Lookup(context.Background(), key); ok {
		t.Error("cache should NOT contain a row after error")
	}
}

// pin: when the upstream JSON body doesn't fit the caller's `out` shape,
// GetCached should error and not stash the malformed payload.
func TestClientGetCachedRejectsBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, "this is not JSON")
	}))
	defer srv.Close()
	c := openCache(t)
	cl := forgejo.New(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})
	var out map[string]string
	if err := cl.GetCached(context.Background(), "/x", &out, cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}, time.Minute); err == nil {
		t.Error("expected decode error")
	}
}
