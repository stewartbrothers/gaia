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

// GitHub REST issue-dependency endpoints (API version 2026-03-10):
//
//   GET    /repos/{o}/{r}/issues/{n}/dependencies/blocked_by
//   POST   /repos/{o}/{r}/issues/{n}/dependencies/blocked_by         body: {"issue_id": <int>}
//   DELETE /repos/{o}/{r}/issues/{n}/dependencies/blocked_by/{id}
//   GET    /repos/{o}/{r}/issues/{n}/dependencies/blocking
//
// Two GitHub-specific nuances vs. Forgejo:
//
//   1. The POST body and DELETE path take `issue_id` — the issue's
//      INTERNAL stable primary key, not the user-facing `number`.
//      Our Provider.AddIssueDependency / RemoveIssueDependency
//      contract takes a `dep int` parameter that callers think of as
//      a number (Forgejo's framing). The github provider resolves
//      number → id via an extra GET /issues/{dep} before the write.
//
//   2. DELETE has no body — the issue_id goes in the URL path.
//      Forgejo puts it in a body. Wrapper hides the difference.

func TestListIssueDependenciesGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42/dependencies/blocked_by" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			makeIssue(7, "blocker one", "open"),
			makeIssue(8, "blocker two", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssueDependencies(context.Background(), "o", "r", 42, provider.ListIssueDepsOptions{})
	if err != nil {
		t.Fatalf("ListIssueDependencies: %v", err)
	}
	if len(got) != 2 || got[0].Number != 7 || got[1].Number != 8 {
		t.Errorf("returned: %+v", got)
	}
	if got[0].Body != "" {
		t.Errorf("Body should be trimmed on list; got %q", got[0].Body)
	}
}

func TestListIssueDependenciesGHNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListIssueDependencies(context.Background(), "o", "r", 999, provider.ListIssueDepsOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}

func TestListIssueBlocksGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/7/dependencies/blocking" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			makeIssue(42, "downstream", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssueBlocks(context.Background(), "o", "r", 7, provider.ListIssueDepsOptions{})
	if err != nil {
		t.Fatalf("ListIssueBlocks: %v", err)
	}
	if len(got) != 1 || got[0].Number != 42 {
		t.Errorf("returned: %+v", got)
	}
}

// TestAddIssueDependencyGH pins the number→ID resolution + POST body
// shape. The caller passes dep=7 (the issue NUMBER); the provider
// first GETs /issues/7 to learn its internal `id` (e.g. 12345), then
// POSTs to /issues/42/dependencies/blocked_by with body
// {"issue_id": 12345}.
func TestAddIssueDependencyGH(t *testing.T) {
	const (
		blockerNumber = 7
		blockerID     = 12345
	)
	var (
		gotResolvePath string
		gotPostPath    string
		gotPostBody    map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues/7"):
			gotResolvePath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         blockerID,
				"number":     blockerNumber,
				"title":      "blocker",
				"state":      "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-02T00:00:00Z",
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/dependencies/blocked_by"):
			gotPostPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotPostBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(makeIssue(blockerNumber, "blocker", "open"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	added, err := p.AddIssueDependency(context.Background(), "o", "r", 42, blockerNumber)
	if err != nil {
		t.Fatalf("AddIssueDependency: %v", err)
	}
	if added == nil || added.Number != blockerNumber {
		t.Errorf("returned blocker: %+v", added)
	}
	if gotResolvePath != "/repos/o/r/issues/7" {
		t.Errorf("resolve path: got %q", gotResolvePath)
	}
	if gotPostPath != "/repos/o/r/issues/42/dependencies/blocked_by" {
		t.Errorf("post path: got %q", gotPostPath)
	}
	// Body must use issue_id (the internal ID), NOT the issue number.
	if id, ok := gotPostBody["issue_id"]; !ok {
		t.Errorf("body missing issue_id field: %+v", gotPostBody)
	} else if int(id.(float64)) != blockerID {
		t.Errorf("body issue_id: got %v, want %d (internal id, not number)", id, blockerID)
	}
}

// TestAddIssueDependencyGHResolveFails pins the failure mode where
// the number→ID GET itself errors (e.g. the issue doesn't exist).
// The error should surface as NotFound and the POST should never
// run.
func TestAddIssueDependencyGHResolveFails(t *testing.T) {
	var postCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			postCalled = true
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.AddIssueDependency(context.Background(), "o", "r", 42, 999)
	if err == nil {
		t.Fatal("expected error when resolve GET 404s")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
	if postCalled {
		t.Error("POST must not run when number→ID resolve fails")
	}
}

// TestRemoveIssueDependencyGH pins the DELETE-with-ID-in-path
// shape: number→ID resolve, then DELETE
// /issues/42/dependencies/blocked_by/{id}.
func TestRemoveIssueDependencyGH(t *testing.T) {
	const (
		blockerNumber = 7
		blockerID     = 12345
	)
	var gotDeletePath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues/7"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         blockerID,
				"number":     blockerNumber,
				"title":      "blocker",
				"state":      "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-02T00:00:00Z",
			})
		case r.Method == http.MethodDelete:
			gotDeletePath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(makeIssue(blockerNumber, "removed", "open"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.RemoveIssueDependency(context.Background(), "o", "r", 42, blockerNumber); err != nil {
		t.Fatalf("RemoveIssueDependency: %v", err)
	}
	wantPath := "/repos/o/r/issues/42/dependencies/blocked_by/12345"
	if gotDeletePath != wantPath {
		t.Errorf("delete path: got %q, want %q", gotDeletePath, wantPath)
	}
}

func TestRemoveIssueDependencyGHNotFound(t *testing.T) {
	// Resolve succeeds, DELETE 404s (the edge doesn't exist).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         99,
				"number":     7,
				"title":      "x",
				"state":      "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-01-01T00:00:00Z",
				"updated_at": "2026-01-02T00:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"dependency not found"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.RemoveIssueDependency(context.Background(), "o", "r", 42, 7)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}
