// cache-bench measures the latency and byte savings of the gaia
// read cache by issuing a 100×issue-view loop with the cache
// enabled, then again with the cache bypassed, and prints the
// comparison.
//
// Default: offline mode with an in-process httptest server. Pass
// `-live` to hit the configured forge instead (requires gaia auth).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/stewartbrothers/gaia/core/cache/sqlite"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

var live = flag.Bool("live", false, "use the configured forge (requires gaia auth)")

func main() {
	flag.Parse()
	if *live {
		fmt.Fprintln(os.Stderr, "live mode is not yet implemented in this scaffold; run offline")
		os.Exit(2)
	}

	// Spin a stub forge that returns a deterministic issue payload
	// and counts upstream calls. Each call has a small simulated
	// latency so the cache hit vs miss difference is measurable.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		// Simulate forge round-trip latency (~50ms).
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("ETag", `"v1"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     42,
			"title":      "Cache benchmark sample issue",
			"body":       "Body of moderate size used to make trim measurable.",
			"state":      "open",
			"user":       map[string]string{"login": "alice"},
			"created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-02T00:00:00Z",
		})
	}))
	defer srv.Close()

	// Cache mode.
	cachePath := filepath.Join(os.TempDir(), fmt.Sprintf("cache-bench-%d.db", time.Now().UnixNano()))
	defer func() { _ = os.Remove(cachePath) }()
	c, err := sqlite.Open(cachePath)
	if err != nil {
		fail("sqlite.Open: %v", err)
	}
	defer func() { _ = c.Close() }()

	cachedClient := forgejo.NewProvider(forgejo.Options{
		BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c,
	})
	uncachedClient := forgejo.NewProvider(forgejo.Options{
		BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond,
	})

	const N = 100
	ctx := context.Background()

	// Warm: one request to populate the cache.
	if _, err := cachedClient.GetIssue(ctx, "o", "r", 42, provider.GetIssueOptions{}); err != nil {
		fail("warm: %v", err)
	}
	atomic.StoreInt32(&hits, 0)

	cachedStart := time.Now()
	for i := 0; i < N; i++ {
		if _, err := cachedClient.GetIssue(ctx, "o", "r", 42, provider.GetIssueOptions{}); err != nil {
			fail("cached iter %d: %v", i, err)
		}
	}
	cachedElapsed := time.Since(cachedStart)
	cachedHits := atomic.LoadInt32(&hits)

	atomic.StoreInt32(&hits, 0)
	uncachedStart := time.Now()
	for i := 0; i < N; i++ {
		if _, err := uncachedClient.GetIssue(ctx, "o", "r", 42, provider.GetIssueOptions{}); err != nil {
			fail("uncached iter %d: %v", i, err)
		}
	}
	uncachedElapsed := time.Since(uncachedStart)
	uncachedHits := atomic.LoadInt32(&hits)

	speedup := float64(uncachedElapsed) / float64(cachedElapsed)
	upstreamReduction := 100.0 * (1.0 - float64(cachedHits)/float64(uncachedHits))

	fmt.Printf("=== cache-bench (offline, N=%d) ===\n", N)
	fmt.Printf("  cached:    %12s   %d upstream calls\n", cachedElapsed, cachedHits)
	fmt.Printf("  uncached:  %12s   %d upstream calls\n", uncachedElapsed, uncachedHits)
	fmt.Printf("  speedup:   %4.1fx     upstream call reduction: %.0f%%\n", speedup, upstreamReduction)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "cache-bench: "+format+"\n", args...)
	os.Exit(1)
}
