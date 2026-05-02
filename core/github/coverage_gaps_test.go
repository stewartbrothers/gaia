package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Closes the per-function coverage gaps the parity matrix promised
// for the GitHub provider. Each test names the method + branch it
// exercises so the next coverage drop has a name to grep.

func TestCreateLabelGH(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		captured, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 5, "name": "release", "color": "00ff00", "description": "ship-blocker",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreateLabel(context.Background(), "o", "r", provider.CreateLabelOptions{
		Name:        "release",
		Color:       "00ff00",
		Description: "ship-blocker",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if out.Name != "release" || out.Color != "00ff00" {
		t.Errorf("got %+v", out)
	}
	if !strings.Contains(string(captured), `"name":"release"`) {
		t.Errorf("body missing name: %s", captured)
	}
}

func TestCreateLabelGHValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.CreateLabel(context.Background(), "o", "r", provider.CreateLabelOptions{
		Name: "", Color: "bad",
	})
	if err == nil {
		t.Fatal("expected error for 422")
	}
}

// TestGetPullRequestWithCommentsGH covers the WithComments branch of
// GetPullRequest, which fetches the issue-comments endpoint after
// the PR record. Pre-#38 only the WithCISummary branch was tested.
func TestGetPullRequestWithCommentsGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "x", "state": "open",
				"user":       map[string]any{"login": "u"},
				"head":       map[string]any{"ref": "f", "sha": "abc"},
				"base":       map[string]any{"ref": "main"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			})
		case strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "user": map[string]any{"login": "alice"},
					"body": "lgtm", "created_at": "2026-04-03T00:00:00Z"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "o", "r", 7, provider.GetPullRequestOptions{
		WithComments: 50,
	})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if len(got.Comments) != 1 || got.Comments[0].Body != "lgtm" {
		t.Errorf("comments: %+v", got.Comments)
	}
}

func TestGetPullRequestNotFoundGH(t *testing.T) {
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

// TestListIssuesPageTruncated exercises the makePage branch where
// returned == limit so Page.Truncated is true and NextCursor advances.
func TestListIssuesPageTruncatedGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Return exactly 3 issues against a limit of 3 so the
		// truncation heuristic fires.
		_ = json.NewEncoder(w).Encode([]map[string]any{
			makeIssue(1, "a", "open"),
			makeIssue(2, "b", "open"),
			makeIssue(3, "c", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, page, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if page == nil {
		t.Fatal("page nil")
	}
	if !page.Truncated {
		t.Errorf("expected Truncated=true; got %+v", page)
	}
	if page.NextCursor != "2" {
		t.Errorf("NextCursor: got %q, want 2", page.NextCursor)
	}
}

// TestListIssuesPageCursorAdvances exercises makePage when the caller
// supplied an explicit cursor — the NextCursor should be cursor+1.
func TestListIssuesPageCursorAdvancesGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			makeIssue(1, "a", "open"),
			makeIssue(2, "b", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, page, err := p.ListIssues(context.Background(), "o", "r", provider.ListIssuesOptions{
		Limit:  2,
		Cursor: "5",
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if page == nil || !page.Truncated || page.NextCursor != "6" {
		t.Errorf("expected NextCursor=6 truncated; got %+v", page)
	}
}
