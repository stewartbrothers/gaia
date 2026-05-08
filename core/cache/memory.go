package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Memory is a pure-Go, in-memory implementation of [Cache]. It honours
// the same TTL/Stale contract as the SQLite impl but never touches
// disk, never opens a SQL driver, and never imports `modernc.org/sqlite`.
//
// Its only consumer is the test suite — every package downstream of
// `core/cache` (forgejo, github, internal/cli) uses it instead of the
// SQLite impl so test binaries don't pay the modernc compile cost.
//
// The zero value is ready to use:
//
//	c := &cache.Memory{}
//	c.Store(ctx, ...)
//
// Concurrent callers are serialised with a single mutex — fine for
// tests; not optimised for high-throughput production traffic.
type Memory struct {
	mu     sync.Mutex
	rows   map[Key]Entry
	lists  map[ListKey]ListEntry
	closed bool
}

// NewMemory returns a fresh, empty in-memory cache. Equivalent to
// `&Memory{}` but reads more naturally at call sites.
func NewMemory() *Memory {
	return &Memory{}
}

// Lookup returns the entry for k, marking it Stale when fetched_at +
// TTL is in the past. Missing key → ok=false, no error.
func (m *Memory) Lookup(_ context.Context, k Key) (Entry, bool, error) {
	if m == nil {
		return Entry{}, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[k]
	if !ok {
		return Entry{}, false, nil
	}
	row.Key = k
	row.Stale = time.Since(row.FetchedAt) > row.TTL
	return row, true, nil
}

// Store inserts or replaces the entry. Empty payload + missing
// kind/owner/id are rejected to match the SQLite impl's contract.
func (m *Memory) Store(_ context.Context, e Entry) error {
	if m == nil {
		return nil
	}
	if e.Key.Kind == "" || e.Key.Owner == "" || e.Key.ID == "" {
		return errors.New("cache: Store: kind/owner/id are required")
	}
	if len(e.Payload) == 0 {
		return errors.New("cache: Store: empty payload rejected")
	}
	if e.FetchedAt.IsZero() {
		e.FetchedAt = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = make(map[Key]Entry)
	}
	m.rows[e.Key] = e
	return nil
}

// Invalidate drops a single key. Missing key is a no-op.
func (m *Memory) Invalidate(_ context.Context, k Key) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, k)
	return nil
}

// Touch bumps fetched_at on an existing key. Returns an error if the
// key is missing — matching the SQLite impl.
func (m *Memory) Touch(_ context.Context, k Key, t time.Time) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[k]
	if !ok {
		return fmt.Errorf("cache: touch: key not found: %s/%s/%s/%s", k.Kind, k.Owner, k.Repo, k.ID)
	}
	row.FetchedAt = t
	m.rows[k] = row
	return nil
}

// InvalidateRepoLists clears every list_index row for (kind, owner,
// repo). Empty kind == flush every kind for the repo (broader sweep
// used by CreateIssue's invalidator).
func (m *Memory) InvalidateRepoLists(_ context.Context, kind, owner, repo string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.lists {
		if k.Owner != owner || k.Repo != repo {
			continue
		}
		if kind != "" && k.Kind != kind {
			continue
		}
		delete(m.lists, k)
	}
	return nil
}

// StoreList inserts or replaces a list-query entry.
func (m *Memory) StoreList(_ context.Context, e ListEntry) error {
	if m == nil {
		return nil
	}
	if e.Key.Kind == "" || e.Key.QueryHash == "" {
		return errors.New("cache: StoreList: kind and query_hash are required")
	}
	if len(e.Payload) == 0 {
		return errors.New("cache: StoreList: empty payload rejected")
	}
	if e.FetchedAt.IsZero() {
		e.FetchedAt = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lists == nil {
		m.lists = make(map[ListKey]ListEntry)
	}
	m.lists[e.Key] = e
	return nil
}

// LookupList returns the list-query entry for k, marking it stale.
func (m *Memory) LookupList(_ context.Context, k ListKey) (ListEntry, bool, error) {
	if m == nil {
		return ListEntry{}, false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.lists[k]
	if !ok {
		return ListEntry{}, false, nil
	}
	row.Key = k
	row.Stale = time.Since(row.FetchedAt) > row.TTL
	return row, true, nil
}

// Scan returns all non-expired object payloads for the given
// (kind, owner, repo) tuple. Entries older than 2×TTL are excluded.
// Returns an empty slice (not an error) when no entries exist.
func (m *Memory) Scan(_ context.Context, kind, owner, repo string) ([][]byte, error) {
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out [][]byte
	for k, e := range m.rows {
		if k.Kind != kind || k.Owner != owner || k.Repo != repo {
			continue
		}
		// Exclude entries older than 2×TTL.
		if time.Since(e.FetchedAt) > 2*e.TTL {
			continue
		}
		// Copy payload slice to prevent aliasing.
		p := make([]byte, len(e.Payload))
		copy(p, e.Payload)
		out = append(out, p)
	}
	return out, nil
}

// Nuke truncates both maps. Used by tests that need a clean slate
// without re-instantiating Memory.
func (m *Memory) Nuke(_ context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = nil
	m.lists = nil
	return nil
}

// Close marks the in-memory cache closed. There are no underlying
// resources to release, so this is a no-op for correctness — it
// exists only to satisfy [Cache].
func (m *Memory) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
