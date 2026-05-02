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

func ghReleaseJSON(id int64, tag, name string, draft, pre bool) map[string]any {
	return map[string]any{
		"id":               id,
		"tag_name":         tag,
		"name":             name,
		"body":             "notes",
		"draft":            draft,
		"prerelease":       pre,
		"author":           map[string]any{"login": "alice"},
		"target_commitish": "main",
		"created_at":       "2026-04-01T00:00:00Z",
		"published_at":     "2026-04-01T01:00:00Z",
	}
}

func TestListReleasesGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") == "" {
			t.Errorf("missing per_page; got %v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			ghReleaseJSON(1, "v1.0.0", "First", false, false),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListReleases(context.Background(), "o", "r", provider.ListReleasesOptions{})
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	if len(got) != 1 || got[0].TagName != "v1.0.0" {
		t.Errorf("got %+v", got)
	}
}

func TestCreateReleaseGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"tag_name":"v2.0.0"`) {
			t.Errorf("body: %s", body)
		}
		_ = json.NewEncoder(w).Encode(ghReleaseJSON(99, "v2.0.0", "Big", false, false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreateRelease(context.Background(), "o", "r", provider.CreateReleaseOptions{
		TagName: "v2.0.0",
		Name:    "Big",
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if out.TagName != "v2.0.0" {
		t.Errorf("got %+v", out)
	}
}

func TestEditReleaseGHByTag(t *testing.T) {
	patchPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(ghReleaseJSON(7, "v1.0.0", "Old", false, false))
		case r.Method == http.MethodPatch:
			patchPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(ghReleaseJSON(7, "v1.0.0", "New", false, false))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.EditRelease(context.Background(), "o", "r", "v1.0.0", provider.EditReleaseOptions{Name: "New"}); err != nil {
		t.Fatalf("EditRelease: %v", err)
	}
	if patchPath != "/repos/o/r/releases/7" {
		t.Errorf("PATCH path: %q", patchPath)
	}
}

func TestDeleteReleaseGH(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(ghReleaseJSON(7, "v1.0.0", "x", false, false))
		case http.MethodDelete:
			deletePath = r.URL.Path
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteRelease(context.Background(), "o", "r", "v1.0.0"); err != nil {
		t.Fatalf("DeleteRelease: %v", err)
	}
	if deletePath != "/repos/o/r/releases/7" {
		t.Errorf("DELETE path: %q", deletePath)
	}
}
