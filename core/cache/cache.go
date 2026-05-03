// Package cache defines gaia's local read-cache contract. It is the
// trimmed-payload store every HTTP read consults before going upstream
// and the eviction surface every write path hits on success.
//
// Layout: one file per (provider, host) at
//
//	~/.cache/gaia/<provider>/<host>.db
//
// One file per origin gives:
//   - cache-poisoning isolation (a compromised forge can't pollute
//     another forge's cache)
//   - trivial nuke (rm <provider>/<host>.db)
//   - multi-process safety: SQLite's file locking handles concurrent
//     gaia processes sharing the same file.
//
// All payloads are the **trimmed** core/types JSON, never raw forge
// JSON. This keeps the trust-marker contract from #146 intact: an
// Issue.Body cached, retrieved, and re-marshalled through the
// envelope still emits `_trust=external`.
//
// # Decoupling (#158)
//
// This package is interface-only: it defines [Cache], the trimmed
// types ([Key], [Entry], [ListKey], [ListEntry]), the eviction helper
// [Invalidator], the path-resolver [DefaultDir]/[PathFor], and a
// pure-Go in-memory implementation [Memory] used by tests.
//
// The production SQLite implementation lives in
// [github.com/stewartbrothers/gaia/core/cache/sqlite] —
// importing that subpackage pulls `modernc.org/sqlite`. Importing
// only `core/cache` does NOT, which is the whole point of #158:
// downstream packages (`core/forgejo`, `core/github`, `internal/cli`)
// keep their test compile time small.
//
// Production wiring (the only places that import the SQLite impl)
// are `internal/forgebuilder` and `internal/cli/cache.go` (for
// `gaia cache nuke`). Everywhere else takes [Cache] as an opaque
// interface.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// Cache is the contract every cache implementation must satisfy. The
// production implementation is `core/cache/sqlite`; tests use [Memory].
//
// All methods accept a context for cancellation. Implementations are
// expected to be safe for concurrent use by multiple goroutines.
//
// A nil Cache is a programming error in callers — wrap with `if c !=
// nil` at call sites, or use [Memory] for tests where a no-op cache
// is desired.
type Cache interface {
	// Lookup returns the cached entry for k, marking it Stale when
	// older than its TTL. ok=false when no row exists.
	Lookup(ctx context.Context, k Key) (Entry, bool, error)

	// Store inserts (or replaces) a cached object. Empty Payload is
	// rejected — caching empty bytes is almost always a bug.
	Store(ctx context.Context, e Entry) error

	// Invalidate removes a single object key. Missing key is a no-op.
	Invalidate(ctx context.Context, k Key) error

	// Touch updates fetched_at without changing the payload. Used
	// after a 304 Not Modified to confirm the cached bytes are still
	// current. Returns an error when the key is absent.
	Touch(ctx context.Context, k Key, t time.Time) error

	// InvalidateRepoLists clears every list_index row for (kind,
	// owner, repo). Empty kind flushes every kind for the repo.
	InvalidateRepoLists(ctx context.Context, kind, owner, repo string) error

	// StoreList inserts (or replaces) a cached list-query entry.
	StoreList(ctx context.Context, e ListEntry) error

	// LookupList returns the cached list entry for k, marking it
	// Stale when older than its TTL.
	LookupList(ctx context.Context, k ListKey) (ListEntry, bool, error)

	// Nuke truncates every row. Used by `gaia cache nuke` and tests
	// that need a clean slate without recreating the underlying file.
	Nuke(ctx context.Context) error

	// Close releases any resources the implementation holds. Safe to
	// call multiple times. Memory{}'s Close is a no-op.
	Close() error
}

// Key identifies a single cached object: a forge resource scoped to a
// (kind, owner, repo, id) tuple. For owner-scoped resources (e.g.
// packages) Repo is "".
type Key struct {
	Kind  string // "issue", "pr", "comment", "wiki", "release", "package"
	Owner string
	Repo  string // "" for owner-scoped resources
	ID    string // "42", "Home" (slug), "npm/foo/1.0.0", etc.
}

// ListKey identifies a single cached list query: paged-read responses
// are stored under (kind, owner, repo, query_hash). QueryHash is the
// caller-supplied stable hash of the query parameters; HashQuery
// produces canonical hashes for typed query maps.
type ListKey struct {
	Kind      string
	Owner     string
	Repo      string
	QueryHash string
}

// Entry is a stored object plus the metadata Lookup hands back. ETag
// and LastModified populate the conditional-GET headers on the next
// upstream call. Stale is computed at Lookup time: true when fetched
// older than TTL but younger than 2×TTL — caller may choose to serve
// stale + revalidate async (stale-while-revalidate).
type Entry struct {
	Key          Key
	ETag         string
	LastModified string
	FetchedAt    time.Time
	TTL          time.Duration
	Payload      []byte
	Stale        bool
}

// ListEntry is the list-query equivalent of Entry. Payload is opaque
// JSON the caller chose to serialize (typically a slice of trimmed
// types or a slice of object keys to indirect through Lookup).
type ListEntry struct {
	Key        ListKey
	FetchedAt  time.Time
	TTL        time.Duration
	NextCursor string
	Payload    []byte
	Stale      bool
}

// HashQuery produces a deterministic SHA-256 hex digest over a typed
// query map. Used by the HTTP-client wrapper to build a stable
// query_hash from a list call's parameters.
//
// Keys are sorted before hashing so callers don't have to. The digest
// covers the JSON encoding of the sorted (key, value) pairs.
func HashQuery(params map[string]any) string {
	if len(params) == 0 {
		h := sha256.Sum256(nil)
		return hex.EncodeToString(h[:])
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([][2]any, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]any{k, params[k]})
	}
	raw, _ := json.Marshal(pairs)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
