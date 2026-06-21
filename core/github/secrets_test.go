package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

func TestListSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/secrets" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"secrets": []map[string]any{
				{"name": "GH_RELEASE_TOKEN", "created_at": "2026-05-05T12:02:18Z", "updated_at": "2026-05-06T12:02:18Z"},
				{"name": "NPM_TOKEN", "created_at": "2026-05-05T12:02:18Z", "updated_at": "2026-05-05T12:02:18Z"},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListSecrets(context.Background(), "o", "r", provider.ListSecretsOptions{})
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(got) != 2 || got[0].Name != "GH_RELEASE_TOKEN" {
		t.Fatalf("got %+v", got)
	}
	if got[0].UpdatedAt == nil {
		t.Errorf("expected updated_at populated")
	}
}

// TestListSecretsOrg: --org switches to the org-level endpoint.
func TestListSecretsOrg(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/secrets" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 1,
			"secrets":     []map[string]any{{"name": "ORG_TOKEN"}},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListSecrets(context.Background(), "o", "r", provider.ListSecretsOptions{Org: true})
	if err != nil {
		t.Fatalf("ListSecrets org: %v", err)
	}
	if len(got) != 1 || got[0].Name != "ORG_TOKEN" {
		t.Errorf("got %+v", got)
	}
}
