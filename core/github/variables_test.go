package github_test

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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"variables": []map[string]any{
				{"name": "TURBO_TEAM", "value": "acme", "created_at": "2026-05-05T12:02:18Z", "updated_at": "2026-05-06T12:02:18Z"},
				{"name": "TURBO_API", "value": "https://turbo.example.com", "created_at": "2026-05-05T12:02:18Z", "updated_at": "2026-05-05T12:02:18Z"},
			},
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
		t.Errorf("expected value, got %q", got[0].Value)
	}
	if got[0].UpdatedAt == nil {
		t.Errorf("expected updated_at populated")
	}
}

// TestListVariablesOrg: --org switches to the org-level endpoint.
func TestListVariablesOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/variables" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"variables":   []map[string]any{{"name": "ORG_VAR", "value": "shared"}},
		})
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
