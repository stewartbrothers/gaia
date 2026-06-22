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

func ghTagJSON(name, sha string) map[string]any {
	return map[string]any{
		"name":   name,
		"commit": map[string]any{"sha": sha},
	}
}

func TestListTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/tags" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") == "" {
			t.Errorf("missing per_page query: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			ghTagJSON("v1.0.0", "abc123"),
			ghTagJSON("v0.9.0", "def456"),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListTags(context.Background(), "o", "r", provider.ListTagsOptions{})
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(got) != 2 || got[0].Name != "v1.0.0" || got[0].Commit != "abc123" {
		t.Errorf("got %+v", got)
	}
}

// TestCreateTagExplicitFrom: From set → resolve commit SHA → POST ref.
func TestCreateTagExplicitFrom(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/commits/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "deadbeef"})
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/git/refs":
			posted, _ = io.ReadAll(r.Body)
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref":    "refs/tags/v2.0.0",
				"object": map[string]any{"sha": "deadbeef"},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.CreateTag(context.Background(), "o", "r", "v2.0.0", provider.CreateTagOptions{From: "main"})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if !strings.Contains(string(posted), `"ref":"refs/tags/v2.0.0"`) {
		t.Errorf("ref payload wrong: %s", posted)
	}
	if !strings.Contains(string(posted), `"sha":"deadbeef"`) {
		t.Errorf("sha payload wrong: %s", posted)
	}
	if got.Name != "v2.0.0" || got.Commit != "deadbeef" {
		t.Errorf("got %+v", got)
	}
}

// TestCreateTagDefaultFrom: empty From → GET repo for default_branch,
// then resolve + create off it.
func TestCreateTagDefaultFrom(t *testing.T) {
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
				"ref":    "refs/tags/nightly",
				"object": map[string]any{"sha": "cafe"},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.CreateTag(context.Background(), "o", "r", "nightly", provider.CreateTagOptions{}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if resolvedRef != "trunk" {
		t.Errorf("expected to resolve default branch 'trunk', resolved %q", resolvedRef)
	}
}

func TestDeleteTag(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteTag(context.Background(), "o", "r", "v1.0.0"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if method != http.MethodDelete || path != "/repos/o/r/git/refs/tags/v1.0.0" {
		t.Errorf("unexpected %s %s", method, path)
	}
}
