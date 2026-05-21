package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

type fakePR struct {
	Number    int              `json:"number"`
	Title     string           `json:"title"`
	Body      string           `json:"body"`
	State     string           `json:"state"`
	User      map[string]any   `json:"user"`
	Labels    []map[string]any `json:"labels"`
	Head      map[string]any   `json:"head"`
	Base      map[string]any   `json:"base"`
	Merged    bool             `json:"merged"`
	Mergeable *bool            `json:"mergeable,omitempty"`
	Draft     bool             `json:"draft"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	ClosedAt  *time.Time       `json:"closed_at,omitempty"`
	MergedAt  *time.Time       `json:"merged_at,omitempty"`
	HTMLURL   string           `json:"html_url"`
}

func makePR(n int, title, state string, opts func(*fakePR)) fakePR {
	pr := fakePR{
		Number: n, Title: title, State: state,
		User: map[string]any{"login": "alice", "avatar_url": "https://x"},
		Labels: []map[string]any{
			{"name": "p1", "color": "ff0000", "id": 7},
		},
		Head: map[string]any{
			"ref":  "feature/x",
			"sha":  "deadbeef",
			"repo": map[string]any{"full_name": "Gerwood/gaia"},
		},
		Base: map[string]any{
			"ref":  "main",
			"sha":  "cafebabe",
			"repo": map[string]any{"full_name": "Gerwood/gaia"},
		},
		CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
		HTMLURL:   "https://forge.example.com/Gerwood/gaia/pulls/" + strconv.Itoa(n),
	}
	if opts != nil {
		opts(&pr)
	}
	return pr
}

// TestListPullRequestsPreservesHTMLURL pins #305 on the PR list path.
func TestListPullRequestsPreservesHTMLURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]fakePR{makePR(42, "x", "open", nil)})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "Gerwood", "gaia", provider.ListPullRequestsOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	want := "https://forge.example.com/Gerwood/gaia/pulls/42"
	if len(got) != 1 || got[0].HTMLURL != want {
		t.Errorf("HTMLURL: got %q, want %q", got[0].HTMLURL, want)
	}
}

// TestGetPullRequestPreservesHTMLURL pins #305 on the single-PR path.
func TestGetPullRequestPreservesHTMLURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(makePR(7, "y", "open", nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "Gerwood", "gaia", 7, provider.GetPullRequestOptions{})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	want := "https://forge.example.com/Gerwood/gaia/pulls/7"
	if got.HTMLURL != want {
		t.Errorf("HTMLURL: got %q, want %q", got.HTMLURL, want)
	}
}

func TestListPullRequestsBodyOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pr := makePR(1, "has body", "open", nil)
		pr.Body = "full PR description"
		_ = json.NewEncoder(w).Encode([]fakePR{pr})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "Gerwood", "gaia", provider.ListPullRequestsOptions{})
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

func TestListPullRequestsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Gerwood/gaia/pulls" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]fakePR{
			makePR(1, "first", "open", nil),
			makePR(2, "second", "closed", func(p *fakePR) { p.Merged = true }),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, page, err := p.ListPullRequests(context.Background(), "Gerwood", "gaia", provider.ListPullRequestsOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2", len(got))
	}
	if got[0].State != "open" {
		t.Errorf("first state: got %q", got[0].State)
	}
	if got[1].State != "merged" {
		t.Errorf("merged reconciliation: got %q, want merged", got[1].State)
	}
	if got[0].Head.Ref != "feature/x" || got[0].Head.SHA != "deadbeef" {
		t.Errorf("head: got %+v", got[0].Head)
	}
	if got[0].Head.Repo != "" {
		t.Errorf("Head.Repo should be empty for same-repo PRs; got %q", got[0].Head.Repo)
	}
	if page == nil {
		t.Fatal("page should be non-nil")
	}
}

func TestListPullRequestsFilterParams(t *testing.T) {
	var captured url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]fakePR{})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListPullRequests(context.Background(), "o", "r", provider.ListPullRequestsOptions{
		State:  "closed",
		Labels: []string{"p1", "ready"},
		Head:   "feature/x",
		Base:   "main",
		Limit:  10,
		Cursor: "2",
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if got := captured.Get("state"); got != "closed" {
		t.Errorf("state: got %q", got)
	}
	if got := captured.Get("labels"); got != "p1,ready" {
		t.Errorf("labels: got %q", got)
	}
	if got := captured.Get("head"); got != "feature/x" {
		t.Errorf("head: got %q", got)
	}
	if got := captured.Get("base"); got != "main" {
		t.Errorf("base: got %q", got)
	}
	if got := captured.Get("limit"); got != "10" {
		t.Errorf("limit: got %q", got)
	}
	if got := captured.Get("page"); got != "2" {
		t.Errorf("page: got %q", got)
	}
}

func TestListPullRequestsCrossForkPopulatesHeadRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pr := makePR(1, "fork-pr", "open", func(p *fakePR) {
			p.Head["repo"] = map[string]any{"full_name": "Forker/gaia"}
		})
		_ = json.NewEncoder(w).Encode([]fakePR{pr})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "Gerwood", "gaia", provider.ListPullRequestsOptions{})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if got[0].Head.Repo != "Forker/gaia" {
		t.Errorf("cross-fork head.repo: got %q, want Forker/gaia", got[0].Head.Repo)
	}
}

func TestGetPullRequestHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/42" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		mergeable := true
		_ = json.NewEncoder(w).Encode(makePR(42, "the answer", "open", func(p *fakePR) {
			p.Mergeable = &mergeable
		}))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "o", "r", 42, provider.GetPullRequestOptions{})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if got.Number != 42 || got.State != "open" {
		t.Errorf("got %+v", got)
	}
	if got.Mergeable == nil || !*got.Mergeable {
		t.Errorf("mergeable: got %v", got.Mergeable)
	}
	if got.CISummary != nil {
		t.Errorf("CISummary should be nil unless WithCISummary=true; got %+v", got.CISummary)
	}
}

func TestGetPullRequestStateReconciliation(t *testing.T) {
	cases := []struct {
		name     string
		apiState string
		merged   bool
		mergedAt *time.Time
		want     string
	}{
		{"open", "open", false, nil, "open"},
		{"closed-not-merged", "closed", false, nil, "closed"},
		{"merged", "closed", true, ptrTime(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)), "merged"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(makePR(1, "x", c.apiState, func(p *fakePR) {
					p.Merged = c.merged
					p.MergedAt = c.mergedAt
				}))
			}))
			defer srv.Close()

			p := newTestProvider(t, srv.URL)
			got, err := p.GetPullRequest(context.Background(), "o", "r", 1, provider.GetPullRequestOptions{})
			if err != nil {
				t.Fatalf("GetPullRequest: %v", err)
			}
			if got.State != c.want {
				t.Errorf("state: got %q, want %q", got.State, c.want)
			}
		})
	}
}

func TestGetPullRequestWithCISummary(t *testing.T) {
	statusCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(makePR(42, "x", "open", nil))
		case "/repos/o/r/commits/deadbeef/status":
			atomic.AddInt32(&statusCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state": "failure",
				"statuses": []map[string]any{
					{"status": "success", "context": "build"},
					{"status": "success", "context": "test"},
					{"status": "failure", "context": "lint"},
					{"status": "pending", "context": "deploy"},
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "o", "r", 42, provider.GetPullRequestOptions{WithCISummary: true})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if atomic.LoadInt32(&statusCalls) != 1 {
		t.Errorf("expected 1 status call; got %d", statusCalls)
	}
	if got.CISummary == nil {
		t.Fatal("CISummary should be set")
	}
	if got.CISummary.State != "failure" || got.CISummary.Total != 4 ||
		got.CISummary.Successful != 2 || got.CISummary.Failed != 1 || got.CISummary.Pending != 1 {
		t.Errorf("CISummary rollup: got %+v", got.CISummary)
	}
}

func TestGetPullRequestWithoutCISummarySkipsStatusCall(t *testing.T) {
	statusCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(makePR(42, "x", "open", nil))
		case "/repos/o/r/commits/deadbeef/status":
			atomic.AddInt32(&statusCalls, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"state":"success","statuses":[]}`))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetPullRequest(context.Background(), "o", "r", 42, provider.GetPullRequestOptions{}); err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if atomic.LoadInt32(&statusCalls) != 0 {
		t.Errorf("WithCISummary=false must not call /status; got %d calls", statusCalls)
	}
}

func TestGetPullRequestWithComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(makePR(42, "x", "open", nil))
		case "/repos/o/r/issues/42/comments":
			// PR top-level comments live at the issues endpoint —
			// Forgejo treats PRs as issues with extras.
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         101,
					"user":       map[string]any{"login": "alice"},
					"body":       "lgtm",
					"created_at": "2026-04-03T00:00:00Z",
					"updated_at": "2026-04-03T00:00:00Z",
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "o", "r", 42, provider.GetPullRequestOptions{WithComments: 5})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "lgtm" || got.Comments[0].Source != "issue" {
		t.Errorf("comments: got %+v", got.Comments)
	}
}

func TestGetPullRequestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetPullRequest(context.Background(), "o", "r", 999, provider.GetPullRequestOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
