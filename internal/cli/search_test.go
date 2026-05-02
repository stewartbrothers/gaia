package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestSearchJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "memory leak" {
			t.Errorf("query: got %q", r.URL.Query().Get("q"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number":     42,
				"title":      "fix: memory leak in cache",
				"repository": map[string]any{"full_name": "o/r"},
			},
			{
				"number":       101,
				"title":        "feat: cap memory usage",
				"repository":   map[string]any{"full_name": "o/r"},
				"pull_request": map[string]any{},
			},
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
		"search", "memory leak",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data []struct {
			Kind   string `json:"kind"`
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 {
		t.Fatalf("count: got %d", len(env.Data))
	}
	if env.Data[0].Kind != "issue" || env.Data[1].Kind != "pull_request" {
		t.Errorf("kinds: %+v", env.Data)
	}
}

func TestSearchKindFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("type"); got != "issues" {
			t.Errorf("type: got %q, want issues", got)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{})
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
		"search", "x",
		"--kind", "issue",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestSearchPretty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number":     7,
				"title":      "fix: dispatch race",
				"repository": map[string]any{"full_name": "o/r"},
			},
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
		"--format", "pretty",
		"search", "race",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"KIND", "REPO", "issue", "o/r", "#7", "fix: dispatch race"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q; got %q", want, out)
		}
	}
}

func TestSearchEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
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
		"--format", "pretty",
		"search", "nope",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "no results") {
		t.Errorf("expected 'no results'; got %q", stdout.String())
	}
}
