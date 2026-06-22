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

func tagJSON(name, sha, message string) map[string]any {
	return map[string]any{
		"name":    name,
		"message": message,
		"commit":  map[string]any{"sha": sha},
	}
}

func TestListTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/tags" {
			t.Errorf("path: %q", r.URL.Path)
		}
		if r.URL.Query().Get("limit") == "" {
			t.Errorf("missing limit query: %q", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			tagJSON("v1.0.0", "abc123", "release one"),
			tagJSON("v0.9.0", "def456", ""),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListTags(context.Background(), "o", "r", provider.ListTagsOptions{})
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(got) != 2 || got[0].Name != "v1.0.0" || got[0].Commit != "abc123" || got[0].Message != "release one" {
		t.Errorf("got %+v", got)
	}
	if got[1].Name != "v0.9.0" || got[1].Message != "" {
		t.Errorf("got %+v", got[1])
	}
}

func TestCreateTag(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/tags" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		posted, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(tagJSON("v2.0.0", "abc123", ""))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.CreateTag(context.Background(), "o", "r", "v2.0.0", provider.CreateTagOptions{From: "main"})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if !strings.Contains(string(posted), `"tag_name":"v2.0.0"`) {
		t.Errorf("create body missing tag_name: %s", posted)
	}
	if !strings.Contains(string(posted), `"target":"main"`) {
		t.Errorf("create body missing target: %s", posted)
	}
	if got.Name != "v2.0.0" || got.Commit != "abc123" {
		t.Errorf("got %+v", got)
	}
}

// TestCreateTagDefaultsFrom: an empty From omits target so Forgejo tags
// the repo default branch.
func TestCreateTagDefaultsFrom(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(tagJSON("nightly", "abc123", ""))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.CreateTag(context.Background(), "o", "r", "nightly", provider.CreateTagOptions{}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if strings.Contains(string(posted), "target") {
		t.Errorf("empty From should omit target: %s", posted)
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
	if method != http.MethodDelete || path != "/repos/o/r/tags/v1.0.0" {
		t.Errorf("unexpected %s %s", method, path)
	}
}

// TestDeleteTagNotFound: a missing tag maps to NotFound.
func TestDeleteTagNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"tag does not exist"}`))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeleteTag(context.Background(), "o", "r", "missing")
	if err == nil {
		t.Fatal("want error on missing tag, got nil")
	}
	if exitcode.Of(err) != exitcode.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}
