// Package sqlite implements [cache.Cache] backed by a `modernc.org/sqlite`
// (pure-Go, no cgo) database file. It is the production cache impl;
// importing this package pulls the SQLite driver, which is large
// (~234 MB of pure-Go translated C code).
//
// # Why a subpackage (#158)
//
// Before #158, [cache.Cache] was a concrete struct in `core/cache`
// that imported `modernc.org/sqlite`. Every package transitively
// importing `core/cache` (forgejo, github, internal/cli, cmd/*) paid
// the SQLite compile cost on every `go test`. After #158 the
// interface lives in `core/cache` (driver-free) and the SQLite impl
// lives here — only `internal/forgebuilder` and `internal/cli/cache.go`
// (for `gaia cache nuke`) import it. Test compile time downstream
// drops by 5-10 seconds.
//
// # Usage (production wiring)
//
//	c, err := sqlite.Open("/path/to/host.db")
//	if err != nil { ... }
//	defer c.Close()
//	prov := forgejo.NewProvider(forgejo.Options{..., Cache: c})
//
// The returned value satisfies [cache.Cache] and is safe for
// concurrent use; multi-process safety is provided by SQLite's own
// file locking.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/stewartbrothers/gaia/core/cache"
)

// Store is the SQLite-backed implementation of [cache.Cache]. It
// wraps a *sql.DB and serialises multi-statement writes with a mutex.
//
// Most callers don't need to mention this type by name — [Open]
// returns the [cache.Cache] interface so consumers stay implementation-
// agnostic. The concrete type is exported only for tests that want
// to assert on impl-specific properties (file path, file mode).
type Store struct {
	db   *sql.DB
	path string
	mu   sync.Mutex // serialises writes that compose multiple statements
}

// Open returns a [cache.Cache] backed by the SQLite file at path,
// creating the file (and any missing parent directories) with secure
// permissions (0600 for the file, 0700 for the parent dir).
// Re-opening an existing cache file is safe — schema migrations are
// idempotent.
//
// The returned value implements [cache.Cache]; callers who need the
// concrete *Store (e.g. for [Store.Path]) can type-assert.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("cache/sqlite: path is required")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("cache/sqlite: mkdir %s: %w", parent, err)
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
		return nil, fmt.Errorf("cache/sqlite: open %s: %w", path, err)
	}

	// File-mode tighten: sql.Open doesn't actually create the file
	// until first use. Force a no-op statement so the file exists,
	// then chmod.
	if _, err := db.Exec("SELECT 1"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache/sqlite: probe %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)

	s := &Store{db: db, path: path}
	if err := s.applySchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache/sqlite: apply schema: %w", err)
	}
	return s, nil
}

// Close releases the underlying SQLite handle. Safe to call multiple
// times.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the on-disk path the cache file lives at. Useful for
// `gaia cache nuke` debugging output and for tests that assert on
// file location.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Store inserts (or replaces) a cached object. Empty Payload is
// rejected — caching empty bytes is almost always a programming bug.
func (s *Store) Store(ctx context.Context, e cache.Entry) error {
	if s == nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	const q = `INSERT INTO objects(kind, owner, repo, id, etag, last_modified, fetched_at, ttl_seconds, payload)
        VALUES(?,?,?,?,?,?,?,?,?)
        ON CONFLICT(kind, owner, repo, id) DO UPDATE SET
            etag=excluded.etag,
            last_modified=excluded.last_modified,
            fetched_at=excluded.fetched_at,
            ttl_seconds=excluded.ttl_seconds,
            payload=excluded.payload`
	_, err := s.db.ExecContext(ctx, q,
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
func (s *Store) Lookup(ctx context.Context, k cache.Key) (cache.Entry, bool, error) {
	if s == nil {
		return cache.Entry{}, false, nil
	}
	const q = `SELECT etag, last_modified, fetched_at, ttl_seconds, payload
               FROM objects WHERE kind=? AND owner=? AND repo=? AND id=?`
	row := s.db.QueryRowContext(ctx, q, k.Kind, k.Owner, k.Repo, k.ID)
	var (
		etag, lm sql.NullString
		fetched  int64
		ttlSecs  int64
		payload  []byte
	)
	if err := row.Scan(&etag, &lm, &fetched, &ttlSecs, &payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cache.Entry{}, false, nil
		}
		return cache.Entry{}, false, fmt.Errorf("cache: lookup: %w", err)
	}
	fetchedAt := time.Unix(fetched, 0)
	ttl := time.Duration(ttlSecs) * time.Second
	stale := time.Since(fetchedAt) > ttl
	return cache.Entry{
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
func (s *Store) Touch(ctx context.Context, k cache.Key, t time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx,
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
func (s *Store) Invalidate(ctx context.Context, k cache.Key) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
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
func (s *Store) InvalidateRepoLists(ctx context.Context, kind, owner, repo string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == "" {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM list_index WHERE owner=? AND repo=?`, owner, repo)
		if err != nil {
			return fmt.Errorf("cache: invalidate repo lists: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM list_index WHERE kind=? AND owner=? AND repo=?`, kind, owner, repo)
	if err != nil {
		return fmt.Errorf("cache: invalidate repo lists: %w", err)
	}
	return nil
}

// Nuke truncates both tables. Used by `gaia cache nuke` and by tests
// that need a clean slate.
func (s *Store) Nuke(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stmt := range []string{
		`DELETE FROM objects`,
		`DELETE FROM list_index`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("cache: nuke: %w", err)
		}
	}
	return nil
}

// StoreList inserts (or replaces) a cached list-query entry.
func (s *Store) StoreList(ctx context.Context, e cache.ListEntry) error {
	if s == nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	const q = `INSERT INTO list_index(kind, owner, repo, query_hash, fetched_at, ttl_seconds, next_cursor, payload)
        VALUES(?,?,?,?,?,?,?,?)
        ON CONFLICT(kind, owner, repo, query_hash) DO UPDATE SET
            fetched_at=excluded.fetched_at,
            ttl_seconds=excluded.ttl_seconds,
            next_cursor=excluded.next_cursor,
            payload=excluded.payload`
	_, err := s.db.ExecContext(ctx, q,
		e.Key.Kind, e.Key.Owner, e.Key.Repo, e.Key.QueryHash,
		e.FetchedAt.Unix(), int64(e.TTL.Seconds()),
		nullableString(e.NextCursor), e.Payload)
	if err != nil {
		return fmt.Errorf("cache: store list %s/%s/%s/%s: %w", e.Key.Kind, e.Key.Owner, e.Key.Repo, e.Key.QueryHash, err)
	}
	return nil
}

// Scan returns all non-expired object payloads for the given
// (kind, owner, repo) tuple. Used by cache-backed search to avoid
// an upstream round-trip when the cache is warm. Entries older than
// 2×TTL are excluded; entries that are merely stale (past 1×TTL)
// are still returned — the caller can decide whether to use them.
// Returns an empty slice (not an error) when no entries exist.
func (s *Store) Scan(ctx context.Context, kind, owner, repo string) ([][]byte, error) {
	if s == nil {
		return nil, nil
	}
	// fetched_at is stored as Unix seconds (integer). ttl_seconds is the
	// TTL in seconds. An entry is within 2×TTL when:
	//   fetched_at > now_unix - ttl_seconds * 2
	const q = `SELECT payload FROM objects
	           WHERE kind=? AND owner=? AND repo=?
	           AND fetched_at > strftime('%s','now') - ttl_seconds * 2`
	rows, err := s.db.QueryContext(ctx, q, kind, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("cache: scan: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out [][]byte
	for rows.Next() {
		var p []byte
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("cache: scan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cache: scan: %w", err)
	}
	return out, nil
}

// LookupList is the list_index counterpart of Lookup.
func (s *Store) LookupList(ctx context.Context, k cache.ListKey) (cache.ListEntry, bool, error) {
	if s == nil {
		return cache.ListEntry{}, false, nil
	}
	const q = `SELECT fetched_at, ttl_seconds, next_cursor, payload
               FROM list_index WHERE kind=? AND owner=? AND repo=? AND query_hash=?`
	row := s.db.QueryRowContext(ctx, q, k.Kind, k.Owner, k.Repo, k.QueryHash)
	var (
		fetched int64
		ttlSecs int64
		nextCur sql.NullString
		payload []byte
	)
	if err := row.Scan(&fetched, &ttlSecs, &nextCur, &payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cache.ListEntry{}, false, nil
		}
		return cache.ListEntry{}, false, fmt.Errorf("cache: lookup list: %w", err)
	}
	fetchedAt := time.Unix(fetched, 0)
	ttl := time.Duration(ttlSecs) * time.Second
	stale := time.Since(fetchedAt) > ttl
	return cache.ListEntry{
		Key:        k,
		FetchedAt:  fetchedAt,
		TTL:        ttl,
		NextCursor: nextCur.String,
		Payload:    payload,
		Stale:      stale,
	}, true, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
