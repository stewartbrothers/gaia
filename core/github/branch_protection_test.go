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

// ghBPJSON builds a GitHub branch-protection response. GitHub nests the
// required-status-checks contexts and the required-review count under
// their own objects; absent objects (nil) mean "not required".
func ghBPJSON(contexts []string, strict bool, approvals int) map[string]any {
	out := map[string]any{}
	if contexts != nil {
		out["required_status_checks"] = map[string]any{
			"strict":   strict,
			"contexts": contexts,
		}
	}
	if approvals > 0 {
		out["required_pull_request_reviews"] = map[string]any{
			"required_approving_review_count": approvals,
		}
	}
	return out
}

func TestGetBranchProtectionGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/branches/main/protection" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(ghBPJSON([]string{"CI / Build", "CI / Test"}, true, 1))
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

// TestGetBranchProtectionGHChecksForm: GitHub also returns contexts via
// the newer required_status_checks.checks[] form ({context, app_id}).
// gaia reads both, preferring contexts[] but falling back to checks[].
func TestGetBranchProtectionGHChecksForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"required_status_checks": map[string]any{
				"strict": false,
				"checks": []map[string]any{
					{"context": "CI / Build", "app_id": 1},
					{"context": "CI / Test", "app_id": nil},
				},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetBranchProtection(context.Background(), "o", "r", "main")
	if err != nil {
		t.Fatalf("GetBranchProtection: %v", err)
	}
	if len(got.RequiredStatusChecks) != 2 || got.RequiredStatusChecks[1] != "CI / Test" {
		t.Errorf("checks: %+v", got.RequiredStatusChecks)
	}
}

// TestGetBranchProtectionGHNotFound: an unprotected branch is NotFound.
func TestGetBranchProtectionGHNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"Branch not protected"}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.GetBranchProtection(context.Background(), "o", "r", "main"); exitcode.Of(err) != exitcode.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

// TestSetBranchProtectionGH: GitHub uses a declarative PUT carrying the
// full object. Assert the body carries the right contexts/strict/approvals
// and that null-out fields are present for the knobs gaia doesn't model.
func TestSetBranchProtectionGH(t *testing.T) {
	var put []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method: %s (want PUT)", r.Method)
		}
		if r.URL.Path != "/repos/o/r/branches/main/protection" {
			t.Errorf("put path: %q", r.URL.Path)
		}
		put, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(ghBPJSON([]string{"CI / Build"}, true, 2))
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
	body := string(put)
	if !strings.Contains(body, `"contexts":["CI / Build"]`) {
		t.Errorf("PUT body missing contexts: %s", body)
	}
	if !strings.Contains(body, `"strict":true`) {
		t.Errorf("PUT body missing strict: %s", body)
	}
	if !strings.Contains(body, `"required_approving_review_count":2`) {
		t.Errorf("PUT body missing approvals: %s", body)
	}
	if !strings.Contains(body, `"enforce_admins":false`) {
		t.Errorf("PUT body missing enforce_admins: %s", body)
	}
	if !strings.Contains(body, `"restrictions":null`) {
		t.Errorf("PUT body missing restrictions:null: %s", body)
	}
	if got.RequiredApprovals != 2 || !got.StrictStatusChecks {
		t.Errorf("got %+v", got)
	}
}

// TestSetBranchProtectionGHClears: empty opts → required_status_checks and
// required_pull_request_reviews go out as null so the rule actually clears.
func TestSetBranchProtectionGHClears(t *testing.T) {
	var put []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		put, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(ghBPJSON(nil, false, 0))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.SetBranchProtection(context.Background(), "o", "r", "main", provider.SetBranchProtectionOptions{}); err != nil {
		t.Fatalf("SetBranchProtection: %v", err)
	}
	body := string(put)
	if !strings.Contains(body, `"required_status_checks":null`) {
		t.Errorf("PUT body should null status checks: %s", body)
	}
	if !strings.Contains(body, `"required_pull_request_reviews":null`) {
		t.Errorf("PUT body should null reviews: %s", body)
	}
}

func TestDeleteBranchProtectionGH(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/repos/o/r/branches/main/protection" {
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
