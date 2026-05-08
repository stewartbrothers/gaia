package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

func searchResultJSON(number int, title, repo string, isPR bool) map[string]any {
	r := map[string]any{
		"number": number,
		"title":  title,
		"repository": map[string]any{
			"full_name": repo,
		},
	}
	if isPR {
		r["pull_request"] = map[string]any{}
	}
	return r
}

func TestSearchCrossRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/issues/search" {
			t.Errorf("path: got %q, want /repos/issues/search", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "memory leak" {
			t.Errorf("query: got %q", r.URL.Query().Get("q"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			searchResultJSON(1, "fix issue", "Gerwood/gaia", false),
			searchResultJSON(7, "feat: things", "Gerwood/gaia", true),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, page, err := p.Search(context.Background(), "memory leak", provider.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2", len(got))
	}
	if got[0].Kind != "issue" || got[1].Kind != "pull_request" {
		t.Errorf("kind discrimination: %+v", got)
	}
	if got[0].Number != 1 || got[0].Title != "fix issue" || got[0].RepoFull != "Gerwood/gaia" {
		t.Errorf("first: %+v", got[0])
	}
	if page == nil {
		t.Errorf("page should be non-nil")
	}
}

func TestSearchRepoScoped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Gerwood/gaia/issues" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			searchResultJSON(42, "title", "Gerwood/gaia", false),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.Search(context.Background(), "x", provider.SearchOptions{
		Repo: "Gerwood/gaia",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Number != 42 {
		t.Errorf("got %+v", got)
	}
}

func TestSearchKindsFilter(t *testing.T) {
	cases := []struct {
		name string
		kind []string
		want string // expected `type` query param
	}{
		{"issues only", []string{"issue"}, "issues"},
		{"pulls only", []string{"pull_request"}, "pulls"},
		{"both explicit", []string{"issue", "pull_request"}, ""}, // no filter when both
		{"empty defaults", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var captured url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = r.URL.Query()
				_ = json.NewEncoder(w).Encode([]map[string]any{})
			}))
			defer srv.Close()

			p := newTestProvider(t, srv.URL)
			_, _, err := p.Search(context.Background(), "x", provider.SearchOptions{
				Kinds: c.kind,
			})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if got := captured.Get("type"); got != c.want {
				t.Errorf("type: got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSearchPaginationParams(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, _ = p.Search(context.Background(), "x", provider.SearchOptions{
		Limit:  10,
		Cursor: "3",
	})
	if got := captured.Get("limit"); got != "10" {
		t.Errorf("limit: got %q", got)
	}
	if got := captured.Get("page"); got != "3" {
		t.Errorf("page: got %q", got)
	}
}

func TestSearchAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.Search(context.Background(), "x", provider.SearchOptions{})
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("expected Auth on 401; got %d", got)
	}
}

// newTestProviderWithCache builds a Provider backed by an in-memory cache.
func newTestProviderWithCache(t *testing.T, baseURL string, c *cache.Memory) *forgejo.Provider {
	t.Helper()
	return forgejo.NewProvider(forgejo.Options{
		BaseURL:   baseURL,
		Token:     "TEST",
		UserAgent: "gaia-test/1.0",
		RetryWait: 1 * time.Millisecond,
		Cache:     c,
	})
}

// seedIssueInCache stores a minimal apiIssue-shaped payload (what GetCached
// actually caches) under the given cache key.
func seedIssueInCache(t *testing.T, c *cache.Memory, owner, repo string, number int, title, body string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"number": number,
		"title":  title,
		"body":   body,
		"state":  "open",
		"user":   map[string]any{"login": "alice"},
		"labels": []any{},
	})
	if err != nil {
		t.Fatalf("marshal issue: %v", err)
	}
	key := cache.Key{Kind: "issue", Owner: owner, Repo: repo, ID: itoa(number)}
	if err := c.Store(context.Background(), cache.Entry{
		Key:       key,
		FetchedAt: time.Now(),
		TTL:       5 * time.Minute,
		Payload:   payload,
	}); err != nil {
		t.Fatalf("seed issue %d: %v", number, err)
	}
}

// seedPRInCache stores a minimal PR payload under a "pr" cache key.
func seedPRInCache(t *testing.T, c *cache.Memory, owner, repo string, number int, title, body string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"number": number,
		"title":  title,
		"body":   body,
		"state":  "open",
		"user":   map[string]any{"login": "bob"},
		"head":   map[string]any{"ref": "feature", "sha": "abc"},
		"base":   map[string]any{"ref": "main", "sha": "def"},
	})
	if err != nil {
		t.Fatalf("marshal pr: %v", err)
	}
	key := cache.Key{Kind: "pr", Owner: owner, Repo: repo, ID: itoa(number)}
	if err := c.Store(context.Background(), cache.Entry{
		Key:       key,
		FetchedAt: time.Now(),
		TTL:       5 * time.Minute,
		Payload:   payload,
	}); err != nil {
		t.Fatalf("seed pr %d: %v", number, err)
	}
}

// itoa converts an int to string — local copy to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// TestSearchUsesCache: warm cache → upstream NOT called; matching results returned.
func TestSearchUsesCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := openCache(t)
	p := newTestProviderWithCache(t, srv.URL, c)
	owner, repo := "Gerwood", "gaia"

	seedIssueInCache(t, c, owner, repo, 1, "fix memory leak", "this is the body")
	seedIssueInCache(t, c, owner, repo, 2, "add hello feature", "hello world")
	seedIssueInCache(t, c, owner, repo, 3, "unrelated issue", "something else")

	got, _, err := p.Search(context.Background(), "hello", provider.SearchOptions{
		Repo:  owner + "/" + repo,
		Kinds: []string{"issue"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream should not be called when cache is warm; got %d hits", h)
	}

	if len(got) != 1 {
		t.Errorf("got %d results, want 1", len(got))
	}
	if len(got) > 0 && got[0].Number != 2 {
		t.Errorf("expected issue #2 (title matches 'hello'), got #%d", got[0].Number)
	}
	if len(got) > 0 && got[0].Kind != "issue" {
		t.Errorf("kind: got %q want issue", got[0].Kind)
	}
	if len(got) > 0 && got[0].RepoFull != owner+"/"+repo {
		t.Errorf("RepoFull: got %q want %q", got[0].RepoFull, owner+"/"+repo)
	}
}

// TestSearchMatchesBody: query matches body, not just title.
func TestSearchMatchesBody(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := openCache(t)
	p := newTestProviderWithCache(t, srv.URL, c)
	owner, repo := "Gerwood", "gaia"

	seedIssueInCache(t, c, owner, repo, 1, "unrelated title", "body mentions needle keyword")
	seedIssueInCache(t, c, owner, repo, 2, "also unrelated", "no match here")

	got, _, err := p.Search(context.Background(), "needle", provider.SearchOptions{
		Repo:  owner + "/" + repo,
		Kinds: []string{"issue"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream called unexpectedly: %d hits", h)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("got %+v, want issue #1", got)
	}
}

// TestSearchFallsThroughOnColdCache: no cache entries → upstream IS called.
func TestSearchFallsThroughOnColdCache(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			searchResultJSON(5, "upstream result", "Gerwood/gaia", false),
		})
	}))
	defer srv.Close()

	c := openCache(t) // empty cache
	p := newTestProviderWithCache(t, srv.URL, c)

	got, _, err := p.Search(context.Background(), "x", provider.SearchOptions{
		Repo: "Gerwood/gaia",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected 1 upstream hit on cold cache; got %d", h)
	}
	if len(got) != 1 || got[0].Number != 5 {
		t.Errorf("got %+v", got)
	}
}

// TestSearchCrossRepoAlwaysHitsUpstream: opts.Repo == "" → always upstream.
func TestSearchCrossRepoAlwaysHitsUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := openCache(t)
	p := newTestProviderWithCache(t, srv.URL, c)
	// Seed some issues, but cross-repo search should ignore them.
	seedIssueInCache(t, c, "Gerwood", "gaia", 1, "hello world", "")

	_, _, err := p.Search(context.Background(), "hello", provider.SearchOptions{
		Repo: "", // cross-repo → always upstream
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected upstream hit for cross-repo query; got %d", h)
	}
}

// TestSearchNilCacheAlwaysHitsUpstream: no cache wired → upstream call, no panic.
func TestSearchNilCacheAlwaysHitsUpstream(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL) // no cache
	_, _, err := p.Search(context.Background(), "x", provider.SearchOptions{
		Repo: "Gerwood/gaia",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("expected 1 upstream hit when cache is nil; got %d", h)
	}
}

// TestSearchCacheWithPRs: warm cache includes PRs matching query.
func TestSearchCacheWithPRs(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	c := openCache(t)
	p := newTestProviderWithCache(t, srv.URL, c)
	owner, repo := "Gerwood", "gaia"

	seedPRInCache(t, c, owner, repo, 10, "feat: add fuzzy search", "implements fuzzy matching")
	seedPRInCache(t, c, owner, repo, 11, "chore: cleanup", "no matches")

	got, _, err := p.Search(context.Background(), "fuzzy", provider.SearchOptions{
		Repo:  owner + "/" + repo,
		Kinds: []string{"pull_request"},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 0 {
		t.Errorf("upstream called unexpectedly: %d hits", h)
	}
	if len(got) != 1 || got[0].Number != 10 {
		t.Errorf("got %+v", got)
	}
	if len(got) > 0 && got[0].Kind != "pull_request" {
		t.Errorf("kind: got %q want pull_request", got[0].Kind)
	}
}
