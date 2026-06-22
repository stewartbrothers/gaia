package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTagListCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/tags" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "v1.0.0", "commit": map[string]any{"sha": "abc123def456789"}, "message": "release one"},
			{"name": "v0.9.0", "commit": map[string]any{"sha": "ff00"}, "message": ""},
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "tag", "list")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data []struct {
			Name    string `json:"name"`
			Commit  string `json:"commit"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if len(env.Data) != 2 || env.Data[0].Name != "v1.0.0" || env.Data[0].Message != "release one" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestTagCreateCLI(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/tags" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		posted, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "v2.0.0", "commit": map[string]any{"sha": "abc123"}, "message": "",
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "tag", "create", "v2.0.0", "--from", "main")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(string(posted), `"tag_name":"v2.0.0"`) {
		t.Errorf("create body missing tag_name: %s", posted)
	}
	if !strings.Contains(string(posted), `"target":"main"`) {
		t.Errorf("create body missing target: %s", posted)
	}
	if !strings.Contains(stdout, "v2.0.0") {
		t.Errorf("create output missing tag name: %s", stdout)
	}
}

func TestTagDeleteCLINeedsConfirm(t *testing.T) {
	// No server interaction expected without --confirm.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("delete without --confirm must not call the forge")
	}))
	defer srv.Close()

	stdout, _, err := runGaia(t, srv.URL, "tag", "delete", "v1.0.0")
	if err != nil {
		t.Fatalf("delete preview: %v", err)
	}
	if !strings.Contains(stdout, "Would delete") {
		t.Errorf("expected preview text, got %s", stdout)
	}
}

func TestTagDeleteCLIConfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %s", r.Method)
		}
		if r.URL.Path != "/repos/o/r/tags/v1.0.0" {
			t.Errorf("path: %q", r.URL.Path)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "tag", "delete", "v1.0.0", "--confirm")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Deleted tag") {
		t.Errorf("expected deletion confirmation, got %s", stdout)
	}
}
