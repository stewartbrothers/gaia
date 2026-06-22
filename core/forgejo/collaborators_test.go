package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

// TestListCollaborators: Forgejo returns a bare array of users with NO
// inline permission; the provider resolves each user's permission via the
// per-user permission endpoint. One handler serves both paths.
func TestListCollaborators(t *testing.T) {
	perms := map[string]string{"alice": "admin", "bob": "write"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/collaborators":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"login": "alice", "full_name": "Alice A"},
				{"login": "bob", "full_name": "Bob B"},
			})
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/collaborators/") &&
			strings.HasSuffix(r.URL.Path, "/permission"):
			parts := strings.Split(r.URL.Path, "/")
			login := parts[len(parts)-2]
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": perms[login]})
		default:
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListCollaborators(context.Background(), "o", "r", provider.ListCollaboratorsOptions{})
	if err != nil {
		t.Fatalf("ListCollaborators: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 collaborators, got %+v", got)
	}
	if got[0].Login != "alice" || got[0].Permission != "admin" {
		t.Errorf("collaborator[0] = %+v, want alice/admin", got[0])
	}
	if got[1].Login != "bob" || got[1].Permission != "write" {
		t.Errorf("collaborator[1] = %+v, want bob/write", got[1])
	}
}
