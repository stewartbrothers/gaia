package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecretsListCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/secrets" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "GORELEASER_TAP_DEPLOY_KEY", "created_at": "2026-05-06T16:20:32+10:00"},
			{"name": "FORGEJO_RELEASE_TOKEN", "created_at": "2026-05-03T11:07:07+10:00"},
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "secrets", "list")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if len(env.Data) != 2 || env.Data[0].Name != "GORELEASER_TAP_DEPLOY_KEY" {
		t.Errorf("got %+v", env.Data)
	}
	// Values must never appear in output.
	if strings.Contains(stdout, "value") {
		t.Errorf("secret value leaked into output: %s", stdout)
	}
}

func TestSecretsListOrgCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/secrets" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "ORG_TOKEN"}})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "secrets", "list", "--org")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "ORG_TOKEN") {
		t.Errorf("org list missing secret: %s", stdout)
	}
}
