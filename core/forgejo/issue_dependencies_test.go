package forgejo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Forgejo issue-dependency endpoints (inherited from Gitea):
//
//   GET    /repos/{o}/{r}/issues/{n}/dependencies  — issues blocking n
//   POST   /repos/{o}/{r}/issues/{n}/dependencies  — body: {"index": M}
//   DELETE /repos/{o}/{r}/issues/{n}/dependencies  — body: {"index": M}
//
//   GET    /repos/{o}/{r}/issues/{n}/blocks        — issues n blocks
//
// Provider exposes:
//
//   ListIssueDependencies   → GET /dependencies
//   ListIssueBlocks         → GET /blocks
//   AddIssueDependency      → POST /dependencies (body {"index": M})
//   RemoveIssueDependency   → DELETE /dependencies (body {"index": M})
//
// We don't expose Add/Remove on the /blocks endpoint because
// "X blocks Y" ⇔ "Y depends on X" is the same relationship — one
// Add/Remove op (on dependencies) is enough; CLI/MCP can map both
// framings.

func TestListIssueDependenciesHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method: got %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/Gerwood/gaia/issues/42/dependencies" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]fakeIssue{
			makeIssue(7, "blocker one", "open"),
			makeIssue(8, "blocker two", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssueDependencies(context.Background(), "Gerwood", "gaia", 42, provider.ListIssueDepsOptions{})
	if err != nil {
		t.Fatalf("ListIssueDependencies: %v", err)
	}
	if len(got) != 2 || got[0].Number != 7 || got[1].Number != 8 {
		t.Errorf("returned issues: %+v", got)
	}
	// Body should be trimmed on list — same contract as ListIssues.
	if got[0].Body != "" {
		t.Errorf("Body should be trimmed on list: got %q", got[0].Body)
	}
}

func TestListIssueDependenciesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"issue not found"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, _, err := p.ListIssueDependencies(context.Background(), "Gerwood", "gaia", 999, provider.ListIssueDepsOptions{})
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}

func TestListIssueBlocksHappy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Gerwood/gaia/issues/7/blocks" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]fakeIssue{
			makeIssue(42, "blocked thing", "open"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListIssueBlocks(context.Background(), "Gerwood", "gaia", 7, provider.ListIssueDepsOptions{})
	if err != nil {
		t.Fatalf("ListIssueBlocks: %v", err)
	}
	if len(got) != 1 || got[0].Number != 42 {
		t.Errorf("returned issues: %+v", got)
	}
}

// TestAddIssueDependencyHappy pins both the wire shape (POST + body
// {"index": M}) and the round-trip: the added blocker is returned in
// the response.
func TestAddIssueDependencyHappy(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.URL.Path != "/repos/Gerwood/gaia/issues/42/dependencies" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		// Forgejo returns the issue that was added as the blocker.
		_ = json.NewEncoder(w).Encode(makeIssue(7, "the blocker", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.AddIssueDependency(context.Background(), "Gerwood", "gaia", 42, 7)
	if err != nil {
		t.Fatalf("AddIssueDependency: %v", err)
	}
	if got == nil || got.Number != 7 {
		t.Errorf("returned blocker: %+v", got)
	}
	// Body shape must be {"index": <blocker number>} — Forgejo rejects
	// any other shape with 422.
	if idx, ok := capturedBody["index"]; !ok {
		t.Errorf("body missing `index` field: %+v", capturedBody)
	} else if int(idx.(float64)) != 7 {
		t.Errorf("body index: got %v, want 7", idx)
	}
}

func TestAddIssueDependencyAlreadyExists(t *testing.T) {
	// Forgejo returns 409 when the dependency edge already exists.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"dependency already exists"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.AddIssueDependency(context.Background(), "Gerwood", "gaia", 42, 7)
	if err == nil {
		t.Fatal("expected error on 409")
	}
	// 409 doesn't have a dedicated exit code on this path; it should
	// surface as Generic (not silently swallow). MergeConflict (7) is
	// reserved for PR merges per exitcode.go.
	if got := exitcode.Of(err); got != exitcode.Generic {
		t.Errorf("exit code: got %d, want Generic(1)", got)
	}
}

func TestRemoveIssueDependencyHappy(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: got %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/repos/Gerwood/gaia/issues/42/dependencies" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(makeIssue(7, "removed blocker", "open"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.RemoveIssueDependency(context.Background(), "Gerwood", "gaia", 42, 7)
	if err != nil {
		t.Fatalf("RemoveIssueDependency: %v", err)
	}
	if idx, ok := capturedBody["index"]; !ok {
		t.Errorf("body missing `index` field: %+v", capturedBody)
	} else if int(idx.(float64)) != 7 {
		t.Errorf("body index: got %v, want 7", idx)
	}
}

// TestGetIssueWithBlockersHits dependencies pins that
// GetIssueOptions.WithBlockers > 0 triggers a /dependencies fetch
// inlined into the returned Issue.Blockers field.
func TestGetIssueWithBlockersFetchesDependencies(t *testing.T) {
	var depsHit int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42":
			_ = json.NewEncoder(w).Encode(makeIssue(42, "host", "open"))
		case "/repos/o/r/issues/42/dependencies":
			depsHit++
			_ = json.NewEncoder(w).Encode([]fakeIssue{
				makeIssue(7, "blocker", "open"),
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 42, provider.GetIssueOptions{WithBlockers: 5})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if depsHit != 1 {
		t.Errorf("expected 1 /dependencies call; got %d", depsHit)
	}
	if len(got.Blockers) != 1 || got.Blockers[0].Number != 7 {
		t.Errorf("Blockers: got %+v", got.Blockers)
	}
}

// TestGetIssueWithBlockingFetchesBlocks pins the inverse direction:
// WithBlocks > 0 triggers a /blocks fetch into Issue.Blocks.
func TestGetIssueWithBlockingFetchesBlocks(t *testing.T) {
	var blocksHit int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/7":
			_ = json.NewEncoder(w).Encode(makeIssue(7, "host", "open"))
		case "/repos/o/r/issues/7/blocks":
			blocksHit++
			_ = json.NewEncoder(w).Encode([]fakeIssue{
				makeIssue(42, "downstream", "open"),
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "o", "r", 7, provider.GetIssueOptions{WithBlocks: 5})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if blocksHit != 1 {
		t.Errorf("expected 1 /blocks call; got %d", blocksHit)
	}
	if len(got.Blocks) != 1 || got.Blocks[0].Number != 42 {
		t.Errorf("Blocks: got %+v", got.Blocks)
	}
}

func TestRemoveIssueDependencyNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"dependency edge not found"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.RemoveIssueDependency(context.Background(), "Gerwood", "gaia", 42, 999)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}
