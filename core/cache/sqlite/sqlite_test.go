package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/cache/cachetest"
	"github.com/stewartbrothers/gaia/core/cache/sqlite"
)

// openTempStore builds a SQLite-backed store rooted at a fresh
// tempdir. The tempdir is removed by t.Cleanup along with the store.
func openTempStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestSQLiteContract runs the shared [cachetest.RunContract] suite
// against the SQLite-backed store. The same suite runs against
// [cache.Memory] in core/cache — pinning that both implementations
// behave identically for callers.
func TestSQLiteContract(t *testing.T) {
	cachetest.RunContract(t, func(t *testing.T) cache.Cache {
		return openTempStore(t)
	})
}

// TestOpenCreatesFileWith0600AndParentWith0700 pins the on-disk
// permissions: the cache may contain ETags and trimmed payloads with
// PII, so it must be 0600 (owner-only). The parent dir must be 0700
// for the same reason.
func TestOpenCreatesFileWith0600AndParentWith0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix permissions not enforced on windows")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "host.db")
	s, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	parent := filepath.Dir(dbPath)
	pinfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("stat parent: %v", err)
	}
	if pinfo.Mode().Perm() != 0o700 {
		t.Errorf("parent dir mode: got %o, want 0700", pinfo.Mode().Perm())
	}

	finfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if finfo.Mode().Perm() != 0o600 {
		t.Errorf("db file mode: got %o, want 0600", finfo.Mode().Perm())
	}
}

// TestPathReturnsConfiguredFile pins the impl-specific Path()
// accessor. The interface doesn't expose the on-disk path (Memory
// has no path), but `gaia cache nuke` callers and tests need it.
func TestPathReturnsConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if s.Path() != path {
		t.Errorf("Path: got %q want %q", s.Path(), path)
	}
}

// TestOpenRejectsEmptyPath: programmer-error guard.
func TestOpenRejectsEmptyPath(t *testing.T) {
	if _, err := sqlite.Open(""); err == nil {
		t.Error("expected error for empty path")
	}
}

// TestNilStoreMethodsAreNoOps pins that a nil *Store doesn't panic —
// Lookup returns miss, Store/Touch/Invalidate return nil. The
// production wiring constructs nil when caching is disabled (or the
// open fails); this guarantees downstream code can call methods
// without checking for nil.
func TestNilStoreMethodsAreNoOps(t *testing.T) {
	var s *sqlite.Store
	ctx := context.Background()
	if _, ok, err := s.Lookup(ctx, cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}); err != nil || ok {
		t.Errorf("nil Lookup: ok=%v err=%v; want (false, nil)", ok, err)
	}
	if err := s.Store(ctx, cache.Entry{}); err != nil {
		t.Errorf("nil Store: %v", err)
	}
	if err := s.Touch(ctx, cache.Key{}, time.Now()); err != nil {
		t.Errorf("nil Touch: %v", err)
	}
	if err := s.Invalidate(ctx, cache.Key{}); err != nil {
		t.Errorf("nil Invalidate: %v", err)
	}
	if err := s.InvalidateRepoLists(ctx, "issue", "o", "r"); err != nil {
		t.Errorf("nil InvalidateRepoLists: %v", err)
	}
	if err := s.StoreList(ctx, cache.ListEntry{}); err != nil {
		t.Errorf("nil StoreList: %v", err)
	}
	if _, ok, err := s.LookupList(ctx, cache.ListKey{}); err != nil || ok {
		t.Errorf("nil LookupList: ok=%v err=%v; want (false, nil)", ok, err)
	}
	if err := s.Nuke(ctx); err != nil {
		t.Errorf("nil Nuke: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
}

// TestStoreSatisfiesCacheInterface is a compile-time check; if
// *sqlite.Store ever drifts from the [cache.Cache] interface, this
// fails to compile.
func TestStoreSatisfiesCacheInterface(t *testing.T) {
	var _ cache.Cache = (*sqlite.Store)(nil)
}

// TestReopenIsSafe pins the idempotent-schema guarantee: opening an
// existing cache file must not error. This is the multi-process /
// re-deploy case.
func TestReopenIsSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	s1, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.Store(context.Background(), cache.Entry{
		Key:       cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"},
		FetchedAt: time.Now(), TTL: time.Minute, Payload: []byte(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	_ = s1.Close()

	// Reopen — should not error, and the row should still be there.
	s2, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if _, ok, _ := s2.Lookup(context.Background(), cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "1"}); !ok {
		t.Error("row should survive close + reopen")
	}
}
