package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func makeGHPR(n int, state string, merged bool) map[string]any {
	return map[string]any{
		"number": n, "title": "t", "state": state,
		"user":       map[string]any{"login": "alice"},
		"head":       map[string]any{"ref": "feat", "sha": "abc", "repo": map[string]any{"full_name": "o/r"}},
		"base":       map[string]any{"ref": "main", "sha": "def", "repo": map[string]any{"full_name": "o/r"}},
		"merged":     merged,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-02T00:00:00Z",
		"html_url":   "https://github.com/o/r/pull/" + strconv.Itoa(n),
	}
}

// TestListPullRequestsGHPreservesHTMLURL pins #305 on the github
// provider's PR list path: html_url threads through.
func TestListPullRequestsGHPreservesHTMLURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{makeGHPR(42, "open", false)})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "o", "r", provider.ListPullRequestsOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	want := "https://github.com/o/r/pull/42"
	if len(got) != 1 || got[0].HTMLURL != want {
		t.Errorf("HTMLURL: got %q, want %q", got[0].HTMLURL, want)
	}
}

// TestGetPullRequestGHPreservesHTMLURL pins #305 on the single-PR path.
func TestGetPullRequestGHPreservesHTMLURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(makeGHPR(7, "open", false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "o", "r", 7, provider.GetPullRequestOptions{})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	want := "https://github.com/o/r/pull/7"
	if got.HTMLURL != want {
		t.Errorf("HTMLURL: got %q, want %q", got.HTMLURL, want)
	}
}

func TestListPullRequestsGHBodyOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pr := makeGHPR(1, "open", false)
		pr["body"] = "full PR description"
		_ = json.NewEncoder(w).Encode([]map[string]any{pr})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "o", "r", provider.ListPullRequestsOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Body != "" {
		t.Errorf("ListPullRequests must omit Body; got %q", got[0].Body)
	}
}

func TestListPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			makeGHPR(1, "open", false),
			makeGHPR(2, "closed", true),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "o", "r", provider.ListPullRequestsOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[1].State != "merged" {
		t.Errorf("state reconciliation: got %q, want merged", got[1].State)
	}
}

func TestGetPullRequestWithCheckRuns(t *testing.T) {
	statusCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(makeGHPR(42, "open", false))
		case "/repos/o/r/commits/abc/check-runs":
			atomic.AddInt32(&statusCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count": 5,
				"check_runs": []map[string]any{
					{"status": "completed", "conclusion": "success"},
					{"status": "completed", "conclusion": "success"},
					{"status": "completed", "conclusion": "failure"},
					{"status": "completed", "conclusion": "skipped"},
					{"status": "in_progress", "conclusion": ""},
				},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "o", "r", 42, provider.GetPullRequestOptions{
		WithCISummary: true,
	})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if atomic.LoadInt32(&statusCalls) != 1 {
		t.Errorf("expected one check-runs call; got %d", statusCalls)
	}
	if got.CISummary == nil {
		t.Fatal("CISummary should be set")
	}
	// Successful: 2 success + 1 skipped = 3
	// Failed: 1
	// Pending: 1 in_progress
	if got.CISummary.Successful != 3 || got.CISummary.Failed != 1 || got.CISummary.Pending != 1 {
		t.Errorf("rollup: got %+v", got.CISummary)
	}
	// State: any failure → "failure"
	if got.CISummary.State != "failure" {
		t.Errorf("state: got %q, want failure", got.CISummary.State)
	}
}

func TestGetPullRequestWithoutCISummarySkipsCheckRuns(t *testing.T) {
	statusCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(makeGHPR(42, "open", false))
		case "/repos/o/r/commits/abc/check-runs":
			atomic.AddInt32(&statusCalls, 1)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetPullRequest(context.Background(), "o", "r", 42, provider.GetPullRequestOptions{}); err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if atomic.LoadInt32(&statusCalls) != 0 {
		t.Errorf("WithCISummary=false must not call /check-runs; got %d", statusCalls)
	}
}
