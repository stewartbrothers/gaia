// Package cache implements gaia's local read cache. It backs every
// HTTP read with a (kind, owner, repo, id) → trimmed-payload table
// keyed by SQLite, plus a parallel list_index table for paged read
// queries (issue list, PR list, etc.). The cache is opt-in via
// `cache.enabled` in config and bypassable per-call with `--no-cache`.
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
// The driver is `modernc.org/sqlite` — pure Go, no cgo — which keeps
// goreleaser's static cross-compile pipeline (#48) clean. No Go
// build-tag gymnastics required.
package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// Cache wraps a SQLite handle and exposes the read/write helpers the
// HTTP client and write paths use. Safe for concurrent use; the
// underlying *sql.DB pool plus SQLite's own locking serializes writes.
type Cache struct {
	db   *sql.DB
	path string
	mu   sync.Mutex // serializes writes that compose multiple statements
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

// Open returns a Cache backed by the SQLite file at path, creating the
// file (and any missing parent directories) with secure permissions.
// Re-opening an existing cache file is safe — schema migrations are
// idempotent.
func Open(path string) (*Cache, error) {
	if path == "" {
		return nil, errors.New("cache: path is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("cache: mkdir %s: %w", parent, err)
	}
	// Belt-and-braces: the dir might pre-exist with looser perms; tighten.
	_ = os.Chmod(parent, 0o700)

	// `_pragma=journal_mode=WAL` improves multi-process read concurrency
	// (writers don't block readers and vice versa). `busy_timeout`
	// gives concurrent writers a chance to win the lock instead of
	// erroring out.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("cache: open %s: %w", path, err)
	}

	// File-mode tighten: sql.Open doesn't actually create the file
	// until first use. Force a no-op statement so the file exists,
	// then chmod.
	if _, err := db.Exec("SELECT 1"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: probe %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)

	c := &Cache{db: db, path: path}
	if err := c.applySchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: apply schema: %w", err)
	}
	return c, nil
}

// Close releases the underlying SQLite handle.
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Path returns the on-disk path the cache file lives at. Useful for
// `gaia cache nuke` debugging output.
func (c *Cache) Path() string {
	return c.path
}

// Store inserts (or replaces) a cached object. Empty Payload is
// rejected — caching empty bytes is almost always a programming bug.
func (c *Cache) Store(ctx context.Context, e Entry) error {
	if c == nil {
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
	c.mu.Lock()
	defer c.mu.Unlock()
	const q = `INSERT INTO objects(kind, owner, repo, id, etag, last_modified, fetched_at, ttl_seconds, payload)
        VALUES(?,?,?,?,?,?,?,?,?)
        ON CONFLICT(kind, owner, repo, id) DO UPDATE SET
            etag=excluded.etag,
            last_modified=excluded.last_modified,
            fetched_at=excluded.fetched_at,
            ttl_seconds=excluded.ttl_seconds,
            payload=excluded.payload`
	_, err := c.db.ExecContext(ctx, q,
		e.Key.Kind, e.Key.Owner, e.Key.Repo, e.Key.ID,
		nullableString(e.ETag), nullableString(e.LastModified),
		e.FetchedAt.Unix(), int64(e.TTL.Seconds()), e.Payload)
	if err != nil {
		return fmt.Errorf("cache: store %s/%s/%s/%s: %w", e.Key.Kind, e.Key.Owner, e.Key.Repo, e.Key.ID, err)
	}
	return nil
}

// Lookup returns the cached entry for k, marking it Stale when older
// than its TTL. Returns ok=false if no row exists.
func (c *Cache) Lookup(ctx context.Context, k Key) (Entry, bool, error) {
	if c == nil {
		return Entry{}, false, nil
	}
	const q = `SELECT etag, last_modified, fetched_at, ttl_seconds, payload
               FROM objects WHERE kind=? AND owner=? AND repo=? AND id=?`
	row := c.db.QueryRowContext(ctx, q, k.Kind, k.Owner, k.Repo, k.ID)
	var (
		etag, lm sql.NullString
		fetched  int64
		ttlSecs  int64
		payload  []byte
	)
	if err := row.Scan(&etag, &lm, &fetched, &ttlSecs, &payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("cache: lookup: %w", err)
	}
	fetchedAt := time.Unix(fetched, 0)
	ttl := time.Duration(ttlSecs) * time.Second
	stale := time.Since(fetchedAt) > ttl
	return Entry{
		Key:          k,
		ETag:         etag.String,
		LastModified: lm.String,
		FetchedAt:    fetchedAt,
		TTL:          ttl,
		Payload:      payload,
		Stale:        stale,
	}, true, nil
}

// Touch updates the fetched_at timestamp on an existing row without
// changing its payload. Used after a 304 Not Modified, where the
// upstream confirmed the cached payload is current.
func (c *Cache) Touch(ctx context.Context, k Key, t time.Time) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.db.ExecContext(ctx,
		`UPDATE objects SET fetched_at=? WHERE kind=? AND owner=? AND repo=? AND id=?`,
		t.Unix(), k.Kind, k.Owner, k.Repo, k.ID)
	if err != nil {
		return fmt.Errorf("cache: touch: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("cache: touch: key not found: %s/%s/%s/%s", k.Kind, k.Owner, k.Repo, k.ID)
	}
	return nil
}

// Invalidate removes a single object key from the cache. A missing key
// is a no-op — caller doesn't have to pre-check.
func (c *Cache) Invalidate(ctx context.Context, k Key) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.ExecContext(ctx,
		`DELETE FROM objects WHERE kind=? AND owner=? AND repo=? AND id=?`,
		k.Kind, k.Owner, k.Repo, k.ID)
	if err != nil {
		return fmt.Errorf("cache: invalidate: %w", err)
	}
	return nil
}

// InvalidateRepoLists clears every list_index row for (kind, owner,
// repo). Used by mutating provider methods: a new issue could appear
// in any list query, so the safe response is to flush them all. Lists
// are cheap to recompute on next read.
//
// kind=="" means "all kinds" (broader flush, used after CreateIssue
// that might also affect search results).
func (c *Cache) InvalidateRepoLists(ctx context.Context, kind, owner, repo string) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if kind == "" {
		_, err := c.db.ExecContext(ctx,
			`DELETE FROM list_index WHERE owner=? AND repo=?`, owner, repo)
		if err != nil {
			return fmt.Errorf("cache: invalidate repo lists: %w", err)
		}
		return nil
	}
	_, err := c.db.ExecContext(ctx,
		`DELETE FROM list_index WHERE kind=? AND owner=? AND repo=?`, kind, owner, repo)
	if err != nil {
		return fmt.Errorf("cache: invalidate repo lists: %w", err)
	}
	return nil
}

// Nuke truncates both tables. Used by `gaia cache nuke` and by tests
// that need a clean slate.
func (c *Cache) Nuke(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, stmt := range []string{
		`DELETE FROM objects`,
		`DELETE FROM list_index`,
	} {
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("cache: nuke: %w", err)
		}
	}
	return nil
}

// StoreList inserts (or replaces) a cached list-query entry.
func (c *Cache) StoreList(ctx context.Context, e ListEntry) error {
	if c == nil {
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
	c.mu.Lock()
	defer c.mu.Unlock()
	const q = `INSERT INTO list_index(kind, owner, repo, query_hash, fetched_at, ttl_seconds, next_cursor, payload)
        VALUES(?,?,?,?,?,?,?,?)
        ON CONFLICT(kind, owner, repo, query_hash) DO UPDATE SET
            fetched_at=excluded.fetched_at,
            ttl_seconds=excluded.ttl_seconds,
            next_cursor=excluded.next_cursor,
            payload=excluded.payload`
	_, err := c.db.ExecContext(ctx, q,
		e.Key.Kind, e.Key.Owner, e.Key.Repo, e.Key.QueryHash,
		e.FetchedAt.Unix(), int64(e.TTL.Seconds()),
		nullableString(e.NextCursor), e.Payload)
	if err != nil {
		return fmt.Errorf("cache: store list %s/%s/%s/%s: %w", e.Key.Kind, e.Key.Owner, e.Key.Repo, e.Key.QueryHash, err)
	}
	return nil
}

// LookupList is the list_index counterpart of Lookup.
func (c *Cache) LookupList(ctx context.Context, k ListKey) (ListEntry, bool, error) {
	if c == nil {
		return ListEntry{}, false, nil
	}
	const q = `SELECT fetched_at, ttl_seconds, next_cursor, payload
               FROM list_index WHERE kind=? AND owner=? AND repo=? AND query_hash=?`
	row := c.db.QueryRowContext(ctx, q, k.Kind, k.Owner, k.Repo, k.QueryHash)
	var (
		fetched int64
		ttlSecs int64
		nextCur sql.NullString
		payload []byte
	)
	if err := row.Scan(&fetched, &ttlSecs, &nextCur, &payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ListEntry{}, false, nil
		}
		return ListEntry{}, false, fmt.Errorf("cache: lookup list: %w", err)
	}
	fetchedAt := time.Unix(fetched, 0)
	ttl := time.Duration(ttlSecs) * time.Second
	stale := time.Since(fetchedAt) > ttl
	return ListEntry{
		Key:        k,
		FetchedAt:  fetchedAt,
		TTL:        ttl,
		NextCursor: nextCur.String,
		Payload:    payload,
		Stale:      stale,
	}, true, nil
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

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
