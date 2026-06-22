package github_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

// TestListCollaborators: GitHub supplies the permission inline via
// role_name; no per-user call is made.
func TestListCollaborators(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/collaborators" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"login":     "alice",
				"role_name": "admin",
				"permissions": map[string]bool{
					"admin": true, "maintain": true, "push": true, "triage": true, "pull": true,
				},
			},
			{
				"login":     "bob",
				"role_name": "write",
				"permissions": map[string]bool{
					"admin": false, "maintain": false, "push": true, "triage": true, "pull": true,
				},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListCollaborators(context.Background(), "o", "r", provider.ListCollaboratorsOptions{})
	if err != nil {
		t.Fatalf("ListCollaborators: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2, got %+v", got)
	}
	if got[0].Login != "alice" || got[0].Permission != "admin" {
		t.Errorf("collaborator[0] = %+v, want alice/admin", got[0])
	}
	if got[1].Login != "bob" || got[1].Permission != "write" {
		t.Errorf("collaborator[1] = %+v, want bob/write", got[1])
	}
}

// TestListCollaboratorsDerivedPermission: when role_name is absent, the
// permission is derived from the highest flag in the permissions map.
func TestListCollaboratorsDerivedPermission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"login": "carol", "permissions": map[string]bool{"push": true, "pull": true}},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListCollaborators(context.Background(), "o", "r", provider.ListCollaboratorsOptions{})
	if err != nil {
		t.Fatalf("ListCollaborators: %v", err)
	}
	if len(got) != 1 || got[0].Permission != "push" {
		t.Errorf("got %+v, want carol/push", got)
	}
}
