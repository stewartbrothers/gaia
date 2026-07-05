package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestMilestoneAssignAttachesEachIssue(t *testing.T) {
	var patched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		patched = append(patched, r.URL.Path)
		n := strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     mustAtoi(n),
			"title":      "x",
			"state":      "open",
			"user":       map[string]any{"login": "alice"},
			"created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-01T00:00:00Z",
		})
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"milestone", "assign", "12", "101", "102", "103",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if len(patched) != 3 {
		t.Fatalf("expected 3 PATCH calls; got %v", patched)
	}
	var env struct {
		Data []struct {
			Issue int  `json:"issue"`
			OK    bool `json:"ok"`
		} `json:"data"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &env)
	if len(env.Data) != 3 {
		t.Fatalf("expected 3 results; got %+v", env.Data)
	}
	for _, r := range env.Data {
		if !r.OK {
			t.Errorf("expected issue %d to succeed; got %+v", r.Issue, r)
		}
	}
}

func TestMilestoneAssignPartialFailureNonZeroExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issues/102") {
			w.WriteHeader(404)
			return
		}
		n := strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     mustAtoi(n),
			"title":      "x",
			"state":      "open",
			"user":       map[string]any{"login": "alice"},
			"created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-01T00:00:00Z",
		})
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"milestone", "assign", "12", "101", "102",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when one issue fails to attach")
	}
	var env struct {
		Data []struct {
			Issue int  `json:"issue"`
			OK    bool `json:"ok"`
		} `json:"data"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &env)
	if len(env.Data) != 2 {
		t.Fatalf("expected both results still reported; got %+v", env.Data)
	}
}

func TestMilestoneAssignRequiresAtLeastOneIssue(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", "http://x",
		"--repo", "o/r",
		"milestone", "assign", "12",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when no issue numbers given")
	}
}

func mustAtoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}
