package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunnersListCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/actions/runners" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "deploy-runner", "status": "online", "busy": false, "labels": []string{"self-hosted", "linux"}},
			{"id": 2, "name": "spare-runner", "status": "offline", "busy": false, "labels": []string{"self-hosted"}},
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "runners", "list")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data []struct {
			Name   string   `json:"name"`
			Status string   `json:"status"`
			Labels []string `json:"labels"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if len(env.Data) != 2 || env.Data[0].Name != "deploy-runner" {
		t.Fatalf("got %+v", env.Data)
	}
	if env.Data[0].Status != "online" || len(env.Data[0].Labels) != 2 {
		t.Errorf("status/labels: %+v", env.Data[0])
	}
}

func TestRunnersListOrgCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/o/actions/runners" {
			t.Errorf("org path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "org-runner", "status": "online"}})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "runners", "list", "--org")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "org-runner") {
		t.Errorf("org list missing runner: %s", stdout)
	}
}
