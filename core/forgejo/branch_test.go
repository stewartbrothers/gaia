package forgejo_test

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

func branchJSON(name, sha string, protected bool) map[string]any {
	return map[string]any{
		"name":      name,
		"commit":    map[string]any{"id": sha},
		"protected": protected,
	}
}

func TestListBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/branches" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("limit") == "" {
			t.Errorf("missing limit query: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			branchJSON("main", "abc123", true),
			branchJSON("dev", "def456", false),
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
	if got[1].Name != "dev" || got[1].Protected {
		t.Errorf("got %+v", got[1])
	}
}

func TestCreateBranch(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/branches" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		posted, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(branchJSON("feature/x", "abc123", false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.CreateBranch(context.Background(), "o", "r", "feature/x", provider.CreateBranchOptions{From: "main"})
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if !strings.Contains(string(posted), `"new_branch_name":"feature/x"`) {
		t.Errorf("create body missing new_branch_name: %s", posted)
	}
	if !strings.Contains(string(posted), `"old_ref_name":"main"`) {
		t.Errorf("create body missing old_ref_name: %s", posted)
	}
	if got.Name != "feature/x" || got.Commit != "abc123" {
		t.Errorf("got %+v", got)
	}
}

// TestCreateBranchDefaultsFrom: an empty From omits old_ref_name so
// Forgejo branches from the repo default.
func TestCreateBranchDefaultsFrom(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(branchJSON("hotfix", "abc123", false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.CreateBranch(context.Background(), "o", "r", "hotfix", provider.CreateBranchOptions{}); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if strings.Contains(string(posted), "old_ref_name") {
		t.Errorf("empty From should omit old_ref_name: %s", posted)
	}
}

// TestCreateBranchConflict: a duplicate branch surfaces the forge error.
func TestCreateBranchConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"branch already exists"}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.CreateBranch(context.Background(), "o", "r", "main", provider.CreateBranchOptions{}); err == nil {
		t.Fatal("want error on conflict, got nil")
	} else if exitcode.Of(err) == exitcode.NotFound {
		t.Errorf("conflict should not map to NotFound: %v", err)
	}
}
