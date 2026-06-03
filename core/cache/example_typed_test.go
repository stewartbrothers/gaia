package cache_test

import (
	"context"
	"fmt"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
)

// ExampleTyped demonstrates the cache-aside view over a Cache: GetOr
// computes a value on a miss, caches it, and returns the cached copy on
// the next call without re-running the (here, expensive) fetch.
func ExampleTyped() {
	type rateLimit struct {
		Remaining int    `json:"remaining"`
		Resource  string `json:"resource"`
	}

	ctx := context.Background()
	limits := cache.Typed[rateLimit]{Cache: cache.NewMemory(), Kind: "ratelimit", TTL: time.Minute}

	fetches := 0
	fetch := func(context.Context) (rateLimit, error) {
		fetches++ // stand-in for an upstream API round-trip
		return rateLimit{Remaining: 4998, Resource: "core"}, nil
	}

	// First call misses → fetch runs and the value is cached.
	got, _ := limits.GetOr(ctx, "", "", "core", fetch)
	fmt.Printf("remaining=%d resource=%s fetches=%d\n", got.Remaining, got.Resource, fetches)

	// Second call hits → fetch is not re-run.
	got, _ = limits.GetOr(ctx, "", "", "core", fetch)
	fmt.Printf("remaining=%d fetches=%d\n", got.Remaining, fetches)

	// Invalidate forces the next call to fetch again.
	_ = limits.Invalidate(ctx, "", "", "core")
	_, _ = limits.GetOr(ctx, "", "", "core", fetch)
	fmt.Printf("after invalidate: fetches=%d\n", fetches)

	// Output:
	// remaining=4998 resource=core fetches=1
	// remaining=4998 fetches=1
	// after invalidate: fetches=2
}
