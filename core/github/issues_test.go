package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

func newTestProvider(t *testing.T, baseURL string) *github.Provider {
	t.Helper()
	return github.NewProvider(github.Options{
		BaseURL:   baseURL,
		Token:     "TEST",
		UserAgent: "gaia-test/1.0",
	})
}

func makeIssue(n int, title, state string) map[string]any {
	return map[string]any{
		"number": n, "title": title, "state": state,
		"user":       map[string]any{"login": "alice"},
		"labels":     []map[string]any{{"name": "bug"}},
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-02T00:00:00Z",
		"html_url":   fmtIssueURL("o", "r", n),
	}
}

// fmtIssueURL builds a github.com-shaped issue UI URL — what GitHub
// puts in `html_url` on every issue/PR response.
func fmtIssueURL(owner, repo string, n int) string {
	return "https://github.com/" + owner + "/" + repo + "/issues/" + strconv.Itoa(n)
}

func makePR(n int, title string) map[string]any {
	// Same shape as makeIssue but with pull_request field set, so the
	// filter sees it as a PR and drops it from /issues results.
	m := makeIssue(n, title, "open")
	m["pull_request"] = map[string]any{}
	return m
}

// TestListIssuesGHPreservesHTMLURL pins #305 on the github provider's
// list path: html_url threads through to types.Issue.HTMLURL.
func TestListIssuesGHPreservesHTMLURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{makeIssue(42, "x", "open")})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	want := "https://github.com/o/r/issues/42"
	if len(got) != 1 || got[0].HTMLURL != want {
		t.Errorf("HTMLURL: got %q, want %q", got[0].HTMLURL, want)
	}
}

// TestGetIssueGHPreservesHTMLURL pins #305 on the single-issue path.
func TestGetIssueGHPreservesHTMLURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(makeIssue(7, "y", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 7, provider.GetIssueOptions{})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	want := "https://github.com/o/r/issues/7"
	if got.HTMLURL != want {
		t.Errorf("HTMLURL: got %q, want %q", got.HTMLURL, want)
	}
}

func TestListIssuesGHBodyOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		issue := makeIssue(1, "has body", "open")
		issue["body"] = "full description here"
		_ = json.NewEncoder(w).Encode([]map[string]any{issue})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{})
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

func TestListIssuesFiltersOutPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			makeIssue(1, "real issue", "open"),
			makePR(2, "actually a PR"),
			makeIssue(3, "another", "closed"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2 (PR filtered)", len(got))
	}
	for _, i := range got {
		if i.Title == "actually a PR" {
			t.Errorf("PR leaked into issue results: %+v", i)
		}
	}
}

func TestListIssuesPassesFilters(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, _ = p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{
		State:    "closed",
		Labels:   []string{"bug", "p1"},
		Assignee: "alice",
		Author:   "bob",
		Limit:    50,
		Cursor:   "3",
	})
	if got := captured.Get("state"); got != "closed" {
		t.Errorf("state: %q", got)
	}
	if got := captured.Get("labels"); got != "bug,p1" {
		t.Errorf("labels: %q", got)
	}
	if got := captured.Get("assignee"); got != "alice" {
		t.Errorf("assignee: %q", got)
	}
	if got := captured.Get("creator"); got != "bob" {
		t.Errorf("creator: %q", got)
	}
	if got := captured.Get("per_page"); got != "50" {
		t.Errorf("per_page: %q", got)
	}
	if got := captured.Get("page"); got != "3" {
		t.Errorf("page: %q", got)
	}
}

func TestGetIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(makeIssue(42, "the answer", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 42, provider.GetIssueOptions{})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Number != 42 || got.Title != "the answer" {
		t.Errorf("got %+v", got)
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
				{"id": 1, "user": map[string]any{"login": "alice"}, "body": "hi",
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-01T00:00:00Z"},
			})
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 42, provider.GetIssueOptions{WithComments: 5})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if commentsHit != 1 || len(got.Comments) != 1 {
		t.Errorf("comments hit: %d; got: %+v", commentsHit, got.Comments)
	}
	if got.Comments[0].Source != "issue" {
		t.Errorf("source: %q", got.Comments[0].Source)
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
