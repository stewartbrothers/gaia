package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	}
}

func makePR(n int, title string) map[string]any {
	// Same shape as makeIssue but with pull_request field set, so the
	// filter sees it as a PR and drops it from /issues results.
	m := makeIssue(n, title, "open")
	m["pull_request"] = map[string]any{}
	return m
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
