package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
)

type person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// counter wraps a fetch func so tests can assert how many times GetOr
// actually invoked the upstream.
func countingFetch(v person, err error, calls *int) func(context.Context) (person, error) {
	return func(context.Context) (person, error) {
		*calls++
		return v, err
	}
}

func TestTypedGetOrMissFetchesOnceAndStores(t *testing.T) {
	ctx := context.Background()
	tc := cache.Typed[person]{Cache: cache.NewMemory(), Kind: "person", TTL: time.Minute}

	calls := 0
	want := person{Name: "Ada", Age: 36}

	got, err := tc.GetOr(ctx, "", "", "ada", countingFetch(want, nil, &calls))
	if err != nil {
		t.Fatalf("GetOr (miss): %v", err)
	}
	if got != want {
		t.Fatalf("GetOr (miss): got %+v, want %+v", got, want)
	}
	if calls != 1 {
		t.Fatalf("fetch called %d times on miss, want 1", calls)
	}
}

func TestTypedGetOrHitDoesNotFetch(t *testing.T) {
	ctx := context.Background()
	tc := cache.Typed[person]{Cache: cache.NewMemory(), Kind: "person", TTL: time.Minute}

	calls := 0
	want := person{Name: "Ada", Age: 36}

	// Prime the cache.
	if _, err := tc.GetOr(ctx, "", "", "ada", countingFetch(want, nil, &calls)); err != nil {
		t.Fatalf("GetOr (prime): %v", err)
	}
	// Second call must hit and not invoke fetch again.
	got, err := tc.GetOr(ctx, "", "", "ada", countingFetch(person{Name: "WRONG"}, nil, &calls))
	if err != nil {
		t.Fatalf("GetOr (hit): %v", err)
	}
	if got != want {
		t.Fatalf("GetOr (hit): got %+v, want cached %+v", got, want)
	}
	if calls != 1 {
		t.Fatalf("fetch called %d times across miss+hit, want 1", calls)
	}
}

func TestTypedGetOrFetchErrorDoesNotPollute(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory()
	tc := cache.Typed[person]{Cache: c, Kind: "person", TTL: time.Minute}

	boom := errors.New("upstream down")
	calls := 0

	// First call fails: error propagates, nothing is stored.
	if _, err := tc.GetOr(ctx, "", "", "ada", countingFetch(person{}, boom, &calls)); !errors.Is(err, boom) {
		t.Fatalf("GetOr (fetch error): got err=%v, want %v", err, boom)
	}

	// Prove the cache wasn't poisoned: a subsequent successful fetch
	// must still run (the failed attempt left no live entry).
	want := person{Name: "Ada", Age: 36}
	got, err := tc.GetOr(ctx, "", "", "ada", countingFetch(want, nil, &calls))
	if err != nil {
		t.Fatalf("GetOr (recover): %v", err)
	}
	if got != want {
		t.Fatalf("GetOr (recover): got %+v, want %+v", got, want)
	}
	if calls != 2 {
		t.Fatalf("fetch called %d times (fail then succeed), want 2", calls)
	}
}

func TestTypedKindIsolation(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory()
	a := cache.Typed[person]{Cache: c, Kind: "a", TTL: time.Minute}
	b := cache.Typed[person]{Cache: c, Kind: "b", TTL: time.Minute}

	callsA, callsB := 0, 0
	if _, err := a.GetOr(ctx, "", "", "k", countingFetch(person{Name: "in-a"}, nil, &callsA)); err != nil {
		t.Fatalf("a.GetOr: %v", err)
	}
	// Same id under a different Kind must miss and fetch its own value.
	got, err := b.GetOr(ctx, "", "", "k", countingFetch(person{Name: "in-b"}, nil, &callsB))
	if err != nil {
		t.Fatalf("b.GetOr: %v", err)
	}
	if got.Name != "in-b" || callsB != 1 {
		t.Fatalf("b.GetOr: got=%+v callsB=%d, want in-b/1 (no cross-Kind alias)", got, callsB)
	}
}

func TestTypedInvalidateForcesRefetch(t *testing.T) {
	ctx := context.Background()
	tc := cache.Typed[person]{Cache: cache.NewMemory(), Kind: "person", TTL: time.Minute}

	calls := 0
	first := person{Name: "Ada", Age: 36}
	if _, err := tc.GetOr(ctx, "", "", "ada", countingFetch(first, nil, &calls)); err != nil {
		t.Fatalf("GetOr (prime): %v", err)
	}
	if err := tc.Invalidate(ctx, "", "", "ada"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	second := person{Name: "Ada", Age: 37}
	got, err := tc.GetOr(ctx, "", "", "ada", countingFetch(second, nil, &calls))
	if err != nil {
		t.Fatalf("GetOr (after invalidate): %v", err)
	}
	if got != second || calls != 2 {
		t.Fatalf("after Invalidate: got=%+v calls=%d, want refreshed value + 2 fetches", got, calls)
	}
	// Invalidate of a missing key is a no-op.
	if err := tc.Invalidate(ctx, "", "", "nope"); err != nil {
		t.Fatalf("Invalidate missing key: %v", err)
	}
}

func TestTypedInvalidateList(t *testing.T) {
	ctx := context.Background()
	tc := cache.Typed[person]{Cache: cache.NewMemory(), Kind: "person", TTL: time.Minute}
	// No list rows stored — InvalidateList must still succeed as a no-op.
	if err := tc.InvalidateList(ctx, "octocat", "hello"); err != nil {
		t.Fatalf("InvalidateList: %v", err)
	}
	if err := tc.InvalidateList(ctx, "", ""); err != nil {
		t.Fatalf("InvalidateList (empty owner/repo): %v", err)
	}
}

func TestTypedGetOrStaleRefetches(t *testing.T) {
	ctx := context.Background()
	// Negative TTL → every stored entry is immediately stale, so each
	// GetOr re-fetches.
	tc := cache.Typed[person]{Cache: cache.NewMemory(), Kind: "person", TTL: -time.Second}

	calls := 0
	if _, err := tc.GetOr(ctx, "", "", "old", countingFetch(person{Name: "x"}, nil, &calls)); err != nil {
		t.Fatalf("GetOr 1: %v", err)
	}
	if _, err := tc.GetOr(ctx, "", "", "old", countingFetch(person{Name: "x"}, nil, &calls)); err != nil {
		t.Fatalf("GetOr 2: %v", err)
	}
	if calls != 2 {
		t.Fatalf("stale entry: fetch called %d times, want 2 (no stale hit)", calls)
	}
}

func TestTypedGetOrCorruptPayloadRefetches(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory()
	// Seed a payload that is valid JSON but the wrong shape for person.
	if err := c.Store(ctx, cache.Entry{
		Key:     cache.Key{Kind: "person", Owner: "_typed", ID: "bad"},
		TTL:     time.Minute,
		Payload: []byte(`[1,2,3]`),
	}); err != nil {
		t.Fatalf("seed Store: %v", err)
	}
	tc := cache.Typed[person]{Cache: c, Kind: "person", TTL: time.Minute}

	calls := 0
	want := person{Name: "Ada"}
	got, err := tc.GetOr(ctx, "", "", "bad", countingFetch(want, nil, &calls))
	if err != nil {
		t.Fatalf("GetOr (corrupt): %v", err)
	}
	if got != want || calls != 1 {
		t.Fatalf("corrupt payload: got=%+v calls=%d, want refetch (value + 1 call)", got, calls)
	}
}

func TestTypedNilCachePassthrough(t *testing.T) {
	ctx := context.Background()
	var c cache.Cache // nil interface value
	tc := cache.Typed[person]{Cache: c, Kind: "person", TTL: time.Minute}

	calls := 0
	want := person{Name: "Ada"}
	got, err := tc.GetOr(ctx, "", "", "ada", countingFetch(want, nil, &calls))
	if err != nil {
		t.Fatalf("GetOr on nil cache: %v", err)
	}
	if got != want {
		t.Fatalf("GetOr on nil cache: got %+v, want %+v", got, want)
	}
	// Nil cache never caches, so a second call fetches again.
	if _, err := tc.GetOr(ctx, "", "", "ada", countingFetch(want, nil, &calls)); err != nil {
		t.Fatalf("GetOr on nil cache (2): %v", err)
	}
	if calls != 2 {
		t.Fatalf("nil cache: fetch called %d times, want 2 (no caching)", calls)
	}
	if err := tc.Invalidate(ctx, "", "", "ada"); err != nil {
		t.Fatalf("Invalidate on nil cache: %v", err)
	}
	if err := tc.InvalidateList(ctx, "", ""); err != nil {
		t.Fatalf("InvalidateList on nil cache: %v", err)
	}
}
