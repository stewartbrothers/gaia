package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cliBPJSON(branch string, contexts []string, approvals int, strict bool) map[string]any {
	return map[string]any{
		"branch_name":              branch,
		"enable_status_check":      len(contexts) > 0,
		"status_check_contexts":    contexts,
		"required_approvals":       approvals,
		"block_on_outdated_branch": strict,
	}
}

func TestBranchListCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/branches" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "main", "commit": map[string]any{"id": "abc123def456789"}, "protected": true},
			{"name": "dev", "commit": map[string]any{"id": "ff00"}, "protected": false},
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "branch", "list")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data []struct {
			Name      string `json:"name"`
			Commit    string `json:"commit"`
			Protected bool   `json:"protected"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if len(env.Data) != 2 || env.Data[0].Name != "main" || !env.Data[0].Protected {
		t.Errorf("got %+v", env.Data)
	}
}

func TestBranchCreateCLI(t *testing.T) {
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/branches" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		posted, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "feature/x", "commit": map[string]any{"id": "abc123"}, "protected": false,
		})
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "branch", "create", "feature/x", "--from", "main")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(string(posted), `"new_branch_name":"feature/x"`) {
		t.Errorf("create body missing new_branch_name: %s", posted)
	}
	if !strings.Contains(string(posted), `"old_ref_name":"main"`) {
		t.Errorf("create body missing old_ref_name: %s", posted)
	}
	if !strings.Contains(stdout, "feature/x") {
		t.Errorf("create output missing branch name: %s", stdout)
	}
}

func TestBranchProtectionGetCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(cliBPJSON("main", []string{"CI / Build"}, 1, true))
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "branch", "protection", "get", "main")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	var env struct {
		Data struct {
			Branch               string   `json:"branch"`
			RequiredStatusChecks []string `json:"required_status_checks"`
			RequiredApprovals    int      `json:"required_approvals"`
		} `json:"data"`
	}
	if e := json.Unmarshal([]byte(stdout), &env); e != nil {
		t.Fatalf("decode: %v\n%s", e, stdout)
	}
	if env.Data.Branch != "main" || env.Data.RequiredApprovals != 1 || len(env.Data.RequiredStatusChecks) != 1 {
		t.Errorf("got %+v", env.Data)
	}
}

func TestBranchProtectionSetCLICreates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(cliBPJSON("main", []string{"CI / Build"}, 0, false))
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL,
		"branch", "protection", "set", "main", "--required-check", "CI / Build")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "CI / Build") {
		t.Errorf("set output missing context: %s", stdout)
	}
}

func TestBranchProtectionDeleteCLINeedsConfirm(t *testing.T) {
	// No server interaction expected without --confirm.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("delete without --confirm must not call the forge")
	}))
	defer srv.Close()

	stdout, _, err := runGaia(t, srv.URL, "branch", "protection", "delete", "main")
	if err != nil {
		t.Fatalf("delete preview: %v", err)
	}
	if !strings.Contains(stdout, "Would delete") {
		t.Errorf("expected preview text, got %s", stdout)
	}
}

func TestBranchProtectionDeleteCLIConfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %s", r.Method)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()

	stdout, stderr, err := runGaia(t, srv.URL, "branch", "protection", "delete", "main", "--confirm")
	if err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Deleted protection") {
		t.Errorf("expected deletion confirmation, got %s", stdout)
	}
}
