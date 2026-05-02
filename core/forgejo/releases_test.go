package forgejo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func releaseJSON(id int64, tag, name string, draft, pre bool) map[string]any {
	return map[string]any{
		"id":               id,
		"tag_name":         tag,
		"name":             name,
		"body":             "release notes",
		"draft":            draft,
		"prerelease":       pre,
		"author":           map[string]any{"login": "alice"},
		"target_commitish": "main",
		"created_at":       "2026-04-01T00:00:00Z",
		"published_at":     "2026-04-01T01:00:00Z",
	}
}

func TestListReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			releaseJSON(1, "v1.0.0", "First release", false, false),
			releaseJSON(2, "v0.9.0-rc1", "RC", false, true),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListReleases(context.Background(), "o", "r", provider.ListReleasesOptions{})
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].TagName != "v1.0.0" || !got[1].Prerelease {
		t.Errorf("got %+v", got)
	}
}

func TestGetReleaseByTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/tags/v1.0.0" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(releaseJSON(1, "v1.0.0", "First", false, false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.GetRelease(context.Background(), "o", "r", "v1.0.0")
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got.TagName != "v1.0.0" {
		t.Errorf("got %+v", got)
	}
}

func TestCreateRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(releaseJSON(99, "v2.0.0", "Big one", false, false))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	out, err := p.CreateRelease(context.Background(), "o", "r", provider.CreateReleaseOptions{
		TagName: "v2.0.0",
		Name:    "Big one",
		Body:    "Notes",
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if out.TagName != "v2.0.0" {
		t.Errorf("got %+v", out)
	}
}

func TestEditReleaseLooksUpByTagFirst(t *testing.T) {
	getCalls := 0
	patchPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/tags/"):
			getCalls++
			_ = json.NewEncoder(w).Encode(releaseJSON(7, "v1.0.0", "Old name", false, false))
		case r.Method == http.MethodPatch:
			patchPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(releaseJSON(7, "v1.0.0", "New name", false, false))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.EditRelease(context.Background(), "o", "r", "v1.0.0", provider.EditReleaseOptions{
		Name: "New name",
	}); err != nil {
		t.Fatalf("EditRelease: %v", err)
	}
	if getCalls != 1 {
		t.Errorf("expected 1 GET to resolve tag→ID; got %d", getCalls)
	}
	if patchPath != "/repos/o/r/releases/7" {
		t.Errorf("PATCH path: got %q", patchPath)
	}
}

func TestDeleteReleaseByTag(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(releaseJSON(7, "v1.0.0", "x", false, false))
		case r.Method == http.MethodDelete:
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
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

func TestGetReleaseNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.GetRelease(context.Background(), "o", "r", "missing")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
