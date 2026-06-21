package github_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func ghBranchJSON(name, sha string, protected bool) map[string]any {
	return map[string]any{
		"name":      name,
		"commit":    map[string]any{"sha": sha},
		"protected": protected,
	}
}

func TestListBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/branches" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") == "" {
			t.Errorf("missing per_page query: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			ghBranchJSON("main", "abc123", true),
			ghBranchJSON("dev", "def456", false),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListBranches(context.Background(), "o", "r", provider.ListBranchesOptions{})
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(got) != 2 || got[0].Name != "main" || got[0].Commit != "abc123" || !got[0].Protected {
		t.Errorf("got %+v", got)
	}
}

// TestCreateBranchExplicitFrom: From set → resolve commit SHA → POST ref.
func TestCreateBranchExplicitFrom(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/commits/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "deadbeef"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/refs":
			posted, _ = io.ReadAll(r.Body)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref":    "refs/heads/feature/x",
				"object": map[string]any{"sha": "deadbeef"},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.CreateBranch(context.Background(), "o", "r", "feature/x", provider.CreateBranchOptions{From: "main"})
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if !strings.Contains(string(posted), `"ref":"refs/heads/feature/x"`) {
		t.Errorf("ref payload wrong: %s", posted)
	}
	if !strings.Contains(string(posted), `"sha":"deadbeef"`) {
		t.Errorf("sha payload wrong: %s", posted)
	}
	if got.Name != "feature/x" || got.Commit != "deadbeef" {
		t.Errorf("got %+v", got)
	}
}

// TestCreateBranchDefaultFrom: empty From → GET repo for default_branch,
// then resolve + create off it.
func TestCreateBranchDefaultFrom(t *testing.T) {
	var resolvedRef string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r":
			_ = json.NewEncoder(w).Encode(map[string]any{"default_branch": "trunk"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/o/r/commits/"):
			resolvedRef = strings.TrimPrefix(r.URL.Path, "/repos/o/r/commits/")
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "cafe"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/refs":
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref":    "refs/heads/hotfix",
				"object": map[string]any{"sha": "cafe"},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.CreateBranch(context.Background(), "o", "r", "hotfix", provider.CreateBranchOptions{}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if resolvedRef != "trunk" {
		t.Errorf("expected to resolve default branch 'trunk', resolved %q", resolvedRef)
	}
}
