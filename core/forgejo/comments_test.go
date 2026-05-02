package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// commentRoutes is a small helper that wires the three Forgejo
// comment endpoints based on a "case" switch — issue (no reviews,
// returns 404 on /pulls), pr (all three sources), prNoInline.
func commentRoutes(t *testing.T, kind string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 1, "user": map[string]any{"login": "alice"},
					"body":       "first thread comment",
					"created_at": "2026-04-01T10:00:00Z",
					"updated_at": "2026-04-01T10:00:00Z",
				},
				{
					"id": 2, "user": map[string]any{"login": "bob"},
					"body":       "second thread comment",
					"created_at": "2026-04-02T10:00:00Z",
					"updated_at": "2026-04-02T10:00:00Z",
				},
			})

		case "/repos/o/r/pulls/42/reviews":
			if kind == "issue" {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":           100,
					"user":         map[string]any{"login": "reviewer"},
					"body":         "looks good overall",
					"state":        "APPROVED",
					"submitted_at": "2026-04-03T10:00:00Z",
				},
				{
					"id":           101,
					"user":         map[string]any{"login": "reviewer"},
					"body":         "", // empty body — pure inline-only review, should be omitted from output
					"state":        "COMMENT",
					"submitted_at": "2026-04-04T10:00:00Z",
				},
			})

		case "/repos/o/r/pulls/42/comments":
			if kind == "issue" || kind == "prNoInline" {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         200,
					"user":       map[string]any{"login": "reviewer"},
					"body":       "tighten this loop",
					"path":       "core/forgejo/issues.go",
					"line":       42,
					"created_at": "2026-04-05T10:00:00Z",
					"updated_at": "2026-04-05T10:00:00Z",
				},
				{
					"id":         201,
					"user":       map[string]any{"login": "reviewer"},
					"body":       "rename this var",
					"path":       "core/forgejo/issues.go",
					"line":       50,
					"created_at": "2026-04-06T10:00:00Z",
					"updated_at": "2026-04-06T10:00:00Z",
				},
			})

		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
}

func TestListCommentsIssueReturnsThreadOnly(t *testing.T) {
	srv := httptest.NewServer(commentRoutes(t, "issue"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2", len(got))
	}
	for _, c := range got {
		if c.Source != "issue" {
			t.Errorf("non-PR comments should all be Source=issue; got %q", c.Source)
		}
	}
}

func TestListCommentsPRMergesAllThreeSources(t *testing.T) {
	srv := httptest.NewServer(commentRoutes(t, "pr"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}

	// Expected: 2 issue + 1 review (the empty-body review is dropped) + 2 inline = 5.
	if len(got) != 5 {
		t.Fatalf("count: got %d, want 5\nentries: %+v", len(got), got)
	}

	// Time-ordered: issue#1 (2026-04-01), issue#2 (04-02), review (04-03), inline#1 (04-05), inline#2 (04-06).
	wantOrder := []struct {
		source string
		body   string
	}{
		{"issue", "first thread comment"},
		{"issue", "second thread comment"},
		{"review", "looks good overall"},
		{"inline", "tighten this loop"},
		{"inline", "rename this var"},
	}
	for i, w := range wantOrder {
		if got[i].Source != w.source || got[i].Body != w.body {
			t.Errorf("[%d]: got source=%q body=%q, want source=%q body=%q",
				i, got[i].Source, got[i].Body, w.source, w.body)
		}
	}

	// Inline comments must carry path+line.
	for _, c := range got {
		if c.Source == "inline" {
			if c.Path == "" || c.Line == 0 {
				t.Errorf("inline comment missing path/line: %+v", c)
			}
		}
	}
}

func TestListCommentsSourceFilter(t *testing.T) {
	srv := httptest.NewServer(commentRoutes(t, "pr"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{
		Sources: []string{"inline"},
	})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: got %d, want 2 (inline only)", len(got))
	}
	for _, c := range got {
		if c.Source != "inline" {
			t.Errorf("got %q, want inline", c.Source)
		}
	}
}

func TestListCommentsRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(commentRoutes(t, "pr"))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListComments(context.Background(), "o", "r", 42, provider.ListCommentsOptions{Limit: 3})
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("limit not honored: got %d, want 3", len(got))
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
		t.Errorf("expected NotFound on missing /issues/42/comments; got %d", got)
	}
}
