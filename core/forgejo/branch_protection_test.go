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

func bpJSON(branch string, enable bool, contexts []string, approvals int, strict bool) map[string]any {
	return map[string]any{
		"branch_name":              branch,
		"enable_status_check":      enable,
		"status_check_contexts":    contexts,
		"required_approvals":       approvals,
		"block_on_outdated_branch": strict,
	}
}

func TestGetBranchProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/branch_protections/main" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(bpJSON("main", true, []string{"CI / Build", "CI / Test"}, 1, true))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetBranchProtection(context.Background(), "o", "r", "main")
	if err != nil {
		t.Fatalf("GetBranchProtection: %v", err)
	}
	if got.Branch != "main" || got.RequiredApprovals != 1 || !got.StrictStatusChecks {
		t.Errorf("got %+v", got)
	}
	if len(got.RequiredStatusChecks) != 2 || got.RequiredStatusChecks[0] != "CI / Build" {
		t.Errorf("contexts: %+v", got.RequiredStatusChecks)
	}
}

// TestGetBranchProtectionNotFound: an unprotected branch is NotFound.
func TestGetBranchProtectionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetBranchProtection(context.Background(), "o", "r", "main"); exitcode.Of(err) != exitcode.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

// TestSetBranchProtectionCreates: no rule yet (GET 404) → POST creates it.
func TestSetBranchProtectionCreates(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case r.Method == http.MethodPost:
			if r.URL.Path != "/repos/o/r/branch_protections" {
				t.Errorf("create path: %q", r.URL.Path)
			}
			posted, _ = io.ReadAll(r.Body)
			_ = json.NewEncoder(w).Encode(bpJSON("main", true, []string{"CI / Build"}, 0, false))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.SetBranchProtection(context.Background(), "o", "r", "main", provider.SetBranchProtectionOptions{
		RequiredStatusChecks: []string{"CI / Build"},
	})
	if err != nil {
		t.Fatalf("SetBranchProtection: %v", err)
	}
	if !strings.Contains(string(posted), `"branch_name":"main"`) {
		t.Errorf("create body missing branch_name: %s", posted)
	}
	if !strings.Contains(string(posted), `"enable_status_check":true`) {
		t.Errorf("create body should enable status check: %s", posted)
	}
	if len(got.RequiredStatusChecks) != 1 {
		t.Errorf("got %+v", got)
	}
}

// TestSetBranchProtectionUpdates: rule exists (GET 200) → PATCH updates it.
func TestSetBranchProtectionUpdates(t *testing.T) {
	var patched []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(bpJSON("main", false, nil, 0, false))
		case http.MethodPatch:
			if r.URL.Path != "/repos/o/r/branch_protections/main" {
				t.Errorf("patch path: %q", r.URL.Path)
			}
			patched, _ = io.ReadAll(r.Body)
			_ = json.NewEncoder(w).Encode(bpJSON("main", true, []string{"CI / Build"}, 2, true))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.SetBranchProtection(context.Background(), "o", "r", "main", provider.SetBranchProtectionOptions{
		RequiredStatusChecks: []string{"CI / Build"},
		RequiredApprovals:    2,
		StrictStatusChecks:   true,
	})
	if err != nil {
		t.Fatalf("SetBranchProtection: %v", err)
	}
	if !strings.Contains(string(patched), `"required_approvals":2`) {
		t.Errorf("patch body missing approvals: %s", patched)
	}
	if got.RequiredApprovals != 2 || !got.StrictStatusChecks {
		t.Errorf("got %+v", got)
	}
}

func TestDeleteBranchProtection(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/repos/o/r/branch_protections/main" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		called = true
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteBranchProtection(context.Background(), "o", "r", "main"); err != nil {
		t.Fatalf("DeleteBranchProtection: %v", err)
	}
	if !called {
		t.Error("delete not called")
	}
}
