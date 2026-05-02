package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func ghCommentRoutes(t *testing.T, kind string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "user": map[string]any{"login": "alice"},
					"body": "first thread", "created_at": "2026-04-01T10:00:00Z",
					"updated_at": "2026-04-01T10:00:00Z"},
			})
		case "/repos/o/r/pulls/42/reviews":
			if kind == "issue" {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 100, "user": map[string]any{"login": "reviewer"},
					"body": "lgtm", "state": "APPROVED",
					"submitted_at": "2026-04-03T10:00:00Z"},
				{"id": 101, "user": map[string]any{"login": "reviewer"},
					"body": "", "state": "COMMENTED",
					"submitted_at": "2026-04-04T10:00:00Z"},
			})
		case "/repos/o/r/pulls/42/comments":
			if kind == "issue" {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 200, "user": map[string]any{"login": "reviewer"},
					"body": "rename", "path": "core/x.go", "line": 42,
					"created_at": "2026-04-05T10:00:00Z",
					"updated_at": "2026-04-05T10:00:00Z"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

func TestListCommentsPRMergesAllSources(t *testing.T) {
	srv := httptest.NewServer(ghCommentRoutes(t, "pr"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	// 1 issue + 1 review (the empty-body one is dropped) + 1 inline = 3
	if len(got) != 3 {
		t.Fatalf("count: got %d, want 3\n%+v", len(got), got)
	}
	wantSrcs := []string{"issue", "review", "inline"}
	for i, want := range wantSrcs {
		if got[i].Source != want {
			t.Errorf("[%d] source: got %q, want %q", i, got[i].Source, want)
		}
	}
}

func TestListCommentsIssueOnly(t *testing.T) {
	srv := httptest.NewServer(ghCommentRoutes(t, "issue"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Source != "issue" {
		t.Errorf("source: %q", got[0].Source)
	}
}

func TestListCommentsSourceFilter(t *testing.T) {
	srv := httptest.NewServer(ghCommentRoutes(t, "pr"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{
		Sources: []string{"inline"},
	})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 1 || got[0].Source != "inline" {
		t.Errorf("filter: got %+v", got)
	}
}

func TestListCommentsThreadEndpointError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
