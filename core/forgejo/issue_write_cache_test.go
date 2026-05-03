package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

// TestEditIssueEvictsCachedRow: a successful PATCH on an issue must
// remove the matching cache row so the next GetIssue refetches.
func TestEditIssueEvictsCachedRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 7, "title": "edited", "state": "open",
			"user": map[string]string{"login": "alice"},
		})
	}))
	defer srv.Close()

	c := cache.NewMemory()

	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Pre-populate the cache row.
	key := cache.Key{Kind: "issue", Owner: "o", Repo: "r", ID: "7"}
	_ = c.Store(context.Background(), cache.Entry{
		Key: key, FetchedAt: time.Now(), TTL: time.Minute,
		Payload: []byte(`{"number":7,"title":"old"}`),
	})

	if _, err := prov.EditIssue(context.Background(), "o", "r", 7, provider.EditIssueOptions{Title: "edited"}); err != nil {
		t.Fatalf("EditIssue: %v", err)
	}

	if _, ok, _ := c.Lookup(context.Background(), key); ok {
		t.Error("EditIssue should evict its cache row")
	}
}

// TestCreateIssueFlushesRepoListIndex: creating an issue invalidates
// the issue-list cache for that repo, since the new issue could appear
// in any list query.
func TestCreateIssueFlushesRepoListIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 99, "title": "new", "state": "open",
			"user": map[string]string{"login": "alice"},
		})
	}))
	defer srv.Close()

	c := cache.NewMemory()

	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})

	// Pre-populate a list cache row for the same repo.
	listKey := cache.ListKey{Kind: "issue", Owner: "o", Repo: "r", QueryHash: "abc"}
	_ = c.StoreList(context.Background(), cache.ListEntry{
		Key: listKey, FetchedAt: time.Now(), TTL: 30 * time.Second,
		Payload: []byte(`["1","2"]`),
	})

	if _, err := prov.CreateIssue(context.Background(), "o", "r", provider.CreateIssueOptions{Title: "new"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if _, ok, _ := c.LookupList(context.Background(), listKey); ok {
		t.Error("CreateIssue should flush issue-list cache for the repo")
	}
}

// TestEditIssueAlsoFlushesLists: editing changes label/state/title and
// could change which lists the issue appears in. Flush.
func TestEditIssueFlushesLists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 5, "title": "edited", "state": "closed",
			"user": map[string]string{"login": "alice"},
		})
	}))
	defer srv.Close()

	c := cache.NewMemory()

	prov := forgejo.NewProvider(forgejo.Options{BaseURL: srv.URL, Token: "X", RetryWait: time.Millisecond, Cache: c})
	listKey := cache.ListKey{Kind: "issue", Owner: "o", Repo: "r", QueryHash: "abc"}
	_ = c.StoreList(context.Background(), cache.ListEntry{
		Key: listKey, FetchedAt: time.Now(), TTL: 30 * time.Second, Payload: []byte(`["5"]`),
	})

	if _, err := prov.EditIssue(context.Background(), "o", "r", 5, provider.EditIssueOptions{State: "closed"}); err != nil {
		t.Fatalf("EditIssue: %v", err)
	}

	if _, ok, _ := c.LookupList(context.Background(), listKey); ok {
		t.Error("EditIssue should flush issue-list cache; state change could move issue between lists")
	}
}
