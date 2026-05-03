package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/cache/cachetest"
)

// TestMemoryContract runs the shared [cachetest.RunContract] suite
// against [cache.Memory]. The same suite runs against the SQLite
// impl in core/cache/sqlite — pinning that both implementations
// behave identically for callers.
func TestMemoryContract(t *testing.T) {
	cachetest.RunContract(t, func(t *testing.T) cache.Cache {
		return cache.NewMemory()
	})
}

// TestMemoryNilSafe pins the contract that a nil *Memory's methods
// degrade gracefully. The CLI's --no-cache path constructs the
// provider without a cache; the helper functions must not panic
// when handed nil.
func TestMemoryNilSafe(t *testing.T) {
	var m *cache.Memory
	ctx := context.Background()
	if _, ok, err := m.Lookup(ctx, cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}); err != nil || ok {
		t.Errorf("nil Lookup: ok=%v err=%v; want (false, nil)", ok, err)
	}
	if err := m.Store(ctx, cache.Entry{}); err != nil {
		t.Errorf("nil Store: %v; want nil", err)
	}
	if err := m.Invalidate(ctx, cache.Key{}); err != nil {
		t.Errorf("nil Invalidate: %v; want nil", err)
	}
	if err := m.Touch(ctx, cache.Key{}, time.Now()); err != nil {
		t.Errorf("nil Touch: %v; want nil", err)
	}
	if err := m.InvalidateRepoLists(ctx, "issue", "o", "r"); err != nil {
		t.Errorf("nil InvalidateRepoLists: %v; want nil", err)
	}
	if err := m.StoreList(ctx, cache.ListEntry{}); err != nil {
		t.Errorf("nil StoreList: %v; want nil", err)
	}
	if _, ok, err := m.LookupList(ctx, cache.ListKey{}); err != nil || ok {
		t.Errorf("nil LookupList: ok=%v err=%v; want (false, nil)", ok, err)
	}
	if err := m.Nuke(ctx); err != nil {
		t.Errorf("nil Nuke: %v; want nil", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("nil Close: %v; want nil", err)
	}
}

// TestNewMemoryReturnsUsableCache pins that the constructor produces
// the same shape the zero value does — no surprises.
func TestNewMemoryReturnsUsableCache(t *testing.T) {
	c := cache.NewMemory()
	ctx := context.Background()
	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}
	if err := c.Store(ctx, cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute, Payload: []byte(`{"x":1}`),
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, ok, _ := c.Lookup(ctx, key); !ok {
		t.Error("expected hit after Store")
	}
}

// TestMemorySatisfiesCacheInterface is a compile-time check; if
// [cache.Memory] ever drifts from the [cache.Cache] interface, this
// fails to compile.
func TestMemorySatisfiesCacheInterface(t *testing.T) {
	var _ cache.Cache = (*cache.Memory)(nil)
	var _ cache.Cache = cache.NewMemory()
}
