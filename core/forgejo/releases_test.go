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

func TestUploadReleaseAsset(t *testing.T) {
	var capturedPath string
	var capturedContentType string
	var capturedBody []byte
	var capturedQueryName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		capturedPath = r.URL.Path
		capturedQueryName = r.URL.Query().Get("name")
		capturedContentType = r.Header.Get("Content-Type")

		// Parse the multipart form so we can inspect the file part.
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		f, hdr, err := r.FormFile("attachment")
		if err != nil {
			t.Fatalf("FormFile attachment: %v", err)
		}
		defer func() { _ = f.Close() }()
		if hdr.Filename != "release-v0.1.0.tar.gz" {
			t.Errorf("filename: got %q", hdr.Filename)
		}
		if got := hdr.Header.Get("Content-Type"); got != "application/gzip" {
			t.Errorf("part Content-Type: got %q", got)
		}
		capturedBody, _ = io.ReadAll(f)
		w.WriteHeader(201)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	body := strings.NewReader("fake-archive-bytes")
	if err := p.UploadReleaseAsset(context.Background(), "o", "r", 7, "release-v0.1.0.tar.gz", "application/gzip", body); err != nil {
		t.Fatalf("UploadReleaseAsset: %v", err)
	}

	if capturedPath != "/repos/o/r/releases/7/assets" {
		t.Errorf("path: %q", capturedPath)
	}
	if capturedQueryName != "release-v0.1.0.tar.gz" {
		t.Errorf("query name: %q", capturedQueryName)
	}
	if !strings.HasPrefix(capturedContentType, "multipart/form-data") {
		t.Errorf("Content-Type should be multipart; got %q", capturedContentType)
	}
	if string(capturedBody) != "fake-archive-bytes" {
		t.Errorf("body: %q", capturedBody)
	}
}

func TestUploadReleaseAssetDefaultsContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		_, hdr, _ := r.FormFile("attachment")
		if got := hdr.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("default Content-Type: got %q, want application/octet-stream", got)
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_ = p.UploadReleaseAsset(context.Background(), "o", "r", 1, "x", "", strings.NewReader("data"))
}

func TestUploadReleaseAssetBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.UploadReleaseAsset(context.Background(), "o", "r", 999, "x", "", strings.NewReader("data"))
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}
