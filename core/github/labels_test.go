package github_test

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

func TestListLabelsGH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "bug", "color": "ff0000", "description": "broken"},
			{"id": 2, "name": "feat", "color": "00ff00", "description": ""},
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListLabels(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[0].Name != "bug" || got[0].Color != "ff0000" {
		t.Errorf("got %+v", got)
	}
	if got[1].ID != 2 {
		t.Errorf("ID should pass through; got %d", got[1].ID)
	}
}

func TestEditLabelGHByName(t *testing.T) {
	patchPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patchPath = r.URL.Path
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "name": "defect", "color": "ff0000",
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if _, err := p.EditLabel(context.Background(), "o", "r", "bug", provider.EditLabelOptions{
		NewName: "defect",
	}); err != nil {
		t.Fatalf("EditLabel: %v", err)
	}
	if !strings.HasSuffix(patchPath, "/labels/bug") {
		t.Errorf("PATCH path: got %q, want suffix /labels/bug (GitHub takes name, not ID)", patchPath)
	}
}

func TestDeleteLabelGH(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletePath = r.URL.Path
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteLabel(context.Background(), "o", "r", "bug"); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}
	if !strings.HasSuffix(deletePath, "/labels/bug") {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

func TestDeleteLabelGHNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	err := p.DeleteLabel(context.Background(), "o", "r", "missing")
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}
