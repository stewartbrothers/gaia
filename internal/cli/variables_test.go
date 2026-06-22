package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVariablesListCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/variables" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "TURBO_TEAM", "data": "acme", "created_at": "2026-05-06T16:20:32+10:00"},
			{"name": "TURBO_API", "data": "https://turbo.example.com", "created_at": "2026-05-03T11:07:07+10:00"},
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "variables", "list")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if len(env.Data) != 2 || env.Data[0].Name != "TURBO_TEAM" {
		t.Errorf("got %+v", env.Data)
	}
	// Variable values ARE returned (unlike secrets).
	if env.Data[0].Value != "acme" {
		t.Errorf("expected value returned, got %q", env.Data[0].Value)
	}
}

func TestVariablesListOrgCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/variables" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "ORG_VAR", "data": "shared"}})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "variables", "list", "--org")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "ORG_VAR") {
		t.Errorf("org list missing variable: %s", stdout)
	}
}
