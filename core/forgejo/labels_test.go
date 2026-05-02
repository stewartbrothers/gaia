package forgejo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func labelJSON(id int64, name, color, desc string) map[string]any {
	return map[string]any{
		"id":          id,
		"name":        name,
		"color":       color,
		"description": desc,
	}
}

func TestListLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/labels" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			labelJSON(1, "bug", "ff0000", "something is broken"),
			labelJSON(2, "feature", "00ff00", ""),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.ListLabels(context.Background(), "o", "r")
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0].Name != "bug" || got[0].Color != "ff0000" || got[0].Description != "something is broken" {
		t.Errorf("got %+v", got[0])
	}
	if got[1].Description != "" {
		t.Errorf("empty description should pass through; got %q", got[1].Description)
	}
}

func TestCreateLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"name":"bug"`) || !strings.Contains(string(body), `"color":"ff0000"`) {
			t.Errorf("body: %s", body)
		}
		_ = json.NewEncoder(w).Encode(labelJSON(99, "bug", "ff0000", ""))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.CreateLabel(context.Background(), "o", "r", provider.CreateLabelOptions{
		Name:  "bug",
		Color: "ff0000",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if got.Name != "bug" || got.Color != "ff0000" {
		t.Errorf("got %+v", got)
	}
}

func TestEditLabelLooksUpIDByName(t *testing.T) {
	listCalls := int32(0)
	patchPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/labels" && r.Method == http.MethodGet:
			atomic.AddInt32(&listCalls, 1)
			_ = json.NewEncoder(w).Encode([]map[string]any{
				labelJSON(7, "bug", "ff0000", ""),
				labelJSON(8, "feature", "00ff00", ""),
			})
		case r.Method == http.MethodPatch:
			patchPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(labelJSON(7, "defect", "ff0000", "renamed"))
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	got, err := p.EditLabel(context.Background(), "o", "r", "bug", provider.EditLabelOptions{
		NewName:     "defect",
		Description: "renamed",
	})
	if err != nil {
		t.Fatalf("EditLabel: %v", err)
	}
	if atomic.LoadInt32(&listCalls) != 1 {
		t.Errorf("expected one list call to resolve name→ID; got %d", listCalls)
	}
	if patchPath != "/repos/o/r/labels/7" {
		t.Errorf("PATCH path: got %q, want /repos/o/r/labels/7", patchPath)
	}
	if got.Name != "defect" {
		t.Errorf("name: %q", got.Name)
	}
}

func TestEditLabelMissingNameIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			labelJSON(1, "bug", "ff0000", ""),
		})
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	_, err := p.EditLabel(context.Background(), "o", "r", "missing", provider.EditLabelOptions{})
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("got %d, want NotFound", got)
	}
}

func TestDeleteLabel(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/labels" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				labelJSON(7, "bug", "ff0000", ""),
			})
		case r.Method == http.MethodDelete:
			deletePath = r.URL.Path
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	if err := p.DeleteLabel(context.Background(), "o", "r", "bug"); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}
	if deletePath != "/repos/o/r/labels/7" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}
