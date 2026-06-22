package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func TestListVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/variables" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"owner_id": 1, "repo_id": 2, "name": "TURBO_TEAM", "data": "acme"},
			{"owner_id": 1, "repo_id": 2, "name": "TURBO_API", "data": "https://turbo.example.com"},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListVariables(context.Background(), "o", "r", provider.ListVariablesOptions{})
	if err != nil {
		t.Fatalf("ListVariables: %v", err)
	}
	if len(got) != 2 || got[0].Name != "TURBO_TEAM" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Value != "acme" {
		t.Errorf("expected value mapped from data, got %q", got[0].Value)
	}
}

// TestListVariablesOrg: --org switches to the org-level endpoint.
func TestListVariablesOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/variables" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "ORG_VAR", "data": "shared"}})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListVariables(context.Background(), "o", "r", provider.ListVariablesOptions{Org: true})
	if err != nil {
		t.Fatalf("ListVariables org: %v", err)
	}
	if len(got) != 1 || got[0].Name != "ORG_VAR" || got[0].Value != "shared" {
		t.Errorf("got %+v", got)
	}
}
