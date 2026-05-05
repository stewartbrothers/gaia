package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
	"github.com/stewartbrothers/gaia/core/provider"
)

// fakeIssue is the Forgejo API shape (just the fields we read).
type fakeIssue struct {
	Number    int              `json:"number"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	State     string           `json:"state"`
	User      map[string]any   `json:"user"`
	Labels    []map[string]any `json:"labels"`
	Assignees []map[string]any `json:"assignees"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	ClosedAt  *time.Time       `json:"closed_at"`
}

func makeIssue(n int, title, state string) fakeIssue {
	return fakeIssue{
		Number: n, Title: title, State: state,
		User:      map[string]any{"login": "alice", "avatar_url": "https://example/a.png"},
		Labels:    []map[string]any{{"name": "bug", "color": "ff0000", "id": 1}},
		Assignees: []map[string]any{{"login": "bob"}},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func newTestProvider(t *testing.T, baseURL string) *forgejo.Provider {
	t.Helper()
	return forgejo.NewProvider(forgejo.Options{
		BaseURL:   baseURL,
		Token:     "TEST",
		UserAgent: "gaia-test/1.0",
		RetryWait: 1 * time.Millisecond,
	})
}

func TestListIssuesBodyOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		issue := makeIssue(1, "has body", "open")
		issue.Body = "this is the full issue description"
		_ = json.NewEncoder(w).Encode([]fakeIssue{issue})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssues(context.Background(), "Gerwood", "gaia", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Body != "" {
		t.Errorf("ListIssues must omit Body; got %q", got[0].Body)
	}
}

func TestListIssuesHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Gerwood/gaia/issues" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if r.URL.Query().Get("type") != "issues" {
			t.Errorf("type query missing: got %q", r.URL.Query().Get("type"))
		}
		_ = json.NewEncoder(w).Encode([]fakeIssue{
			makeIssue(1, "first", "open"),
			makeIssue(2, "second", "closed"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, page, err := p.ListIssues(context.Background(), "Gerwood", "gaia", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2", len(got))
	}
	if got[0].Number != 1 || got[0].Title != "first" || got[0].Author.Login != "alice" {
		t.Errorf("first: got %+v", got[0])
	}
	if len(got[0].Labels) != 1 || got[0].Labels[0].Name != "bug" {
		t.Errorf("labels: got %+v", got[0].Labels)
	}
	// Trim contract: avatar_url, label color, label id must NOT leak.
	if b, _ := json.Marshal(got[0]); string(b) != "" {
		s := string(b)
		for _, banned := range []string{"avatar_url", "ff0000", `"id"`} {
			if contains(s, banned) {
				t.Errorf("trimmed type leaked %q in %s", banned, s)
			}
		}
	}
	if page == nil {
		t.Fatal("page should be non-nil")
	}
	if page.Truncated {
		t.Errorf("2 results with default limit should not truncate")
	}
}

func TestListIssuesPassesFilters(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]fakeIssue{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{
		State:    "closed",
		Labels:   []string{"bug", "p1"},
		Assignee: "alice",
		Author:   "bob",
		Since:    since,
		Query:    "memory leak",
		Limit:    50,
		Cursor:   "3",
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if got := captured.Get("state"); got != "closed" {
		t.Errorf("state: got %q", got)
	}
	if got := captured.Get("labels"); got != "bug,p1" {
		t.Errorf("labels: got %q", got)
	}
	if got := captured.Get("assigned_by"); got != "alice" {
		t.Errorf("assigned_by: got %q", got)
	}
	if got := captured.Get("created_by"); got != "bob" {
		t.Errorf("created_by: got %q", got)
	}
	if got := captured.Get("since"); got != "2026-01-01T00:00:00Z" {
		t.Errorf("since: got %q", got)
	}
	if got := captured.Get("q"); got != "memory leak" {
		t.Errorf("q: got %q", got)
	}
	if got := captured.Get("limit"); got != "50" {
		t.Errorf("limit: got %q", got)
	}
	if got := captured.Get("page"); got != "3" {
		t.Errorf("page: got %q", got)
	}
}

func TestListIssuesPaginationTruncates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return exactly 'limit' items → caller can't tell if more exist
		// upstream; we mark truncated.
		limit := 5
		out := make([]fakeIssue, limit)
		for i := range out {
			out[i] = makeIssue(i+1, "x", "open")
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, page, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{Limit: 5})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if !page.Truncated {
		t.Errorf("expected truncated=true when len==limit")
	}
	if page.NextCursor == "" {
		t.Errorf("expected NextCursor to be set when truncated")
	}
}

func TestListIssuesPaginationDefaultLimit(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]fakeIssue{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, _ = p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{})
	if got := captured.Get("limit"); got != "30" {
		t.Errorf("default limit: got %q, want 30", got)
	}
}

func TestListIssuesNotFoundRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got code %d, want NotFound", got)
	}
}

func TestGetIssueHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(makeIssue(42, "the answer", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 42, provider.GetIssueOptions{})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Number != 42 || got.Title != "the answer" || got.State != "open" {
		t.Errorf("got %+v", got)
	}
	if len(got.Comments) != 0 {
		t.Errorf("comments should be empty when WithComments=0; got %+v", got.Comments)
	}
}

func TestGetIssueWithComments(t *testing.T) {
	commentsHit := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42":
			_ = json.NewEncoder(w).Encode(makeIssue(42, "x", "open"))
		case "/repos/o/r/issues/42/comments":
			commentsHit++
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         101,
					"user":       map[string]any{"login": "alice"},
					"body":       "first",
					"created_at": "2026-01-01T00:00:00Z",
					"updated_at": "2026-01-01T00:00:00Z",
				},
				{
					"id":         102,
					"user":       map[string]any{"login": "bob"},
					"body":       "second",
					"created_at": "2026-01-02T00:00:00Z",
					"updated_at": "2026-01-02T00:00:00Z",
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 42, provider.GetIssueOptions{WithComments: 5})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if commentsHit != 1 {
		t.Errorf("expected one comments fetch; got %d", commentsHit)
	}
	if len(got.Comments) != 2 {
		t.Fatalf("comments: got %d, want 2", len(got.Comments))
	}
	if got.Comments[0].Source != "issue" {
		t.Errorf("source: got %q, want issue", got.Comments[0].Source)
	}
	if got.Comments[0].Body != "first" || got.Comments[1].Body != "second" {
		t.Errorf("comment bodies: got %+v", got.Comments)
	}
}

func TestGetIssueNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetIssue(context.Background(), "o", "r", 999, provider.GetIssueOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
