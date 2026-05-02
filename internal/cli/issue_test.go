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

func TestIssueListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 1, "title": "first", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"labels":     []map[string]any{{"name": "bug"}},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			},
			{
				"number": 2, "title": "second", "state": "closed",
				"user":       map[string]any{"login": "bob"},
				"created_at": "2026-04-03T00:00:00Z",
				"updated_at": "2026-04-04T00:00:00Z",
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
		"--repo", "Gerwood/gaia",
		"issue", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		Data []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			State  string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 || env.Data[0].Number != 1 || env.Data[1].Title != "second" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestIssueListPretty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 42, "title": "hello", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"labels":     []map[string]any{{"name": "bug"}, {"name": "p1"}},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
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
		"issue", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"NUMBER", "STATE", "#42", "open", "hello", "alice", "bug, p1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q; got %q", want, out)
		}
	}
}

func TestIssueListFiltersThreaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != "closed" || q.Get("labels") != "bug" || q.Get("created_by") != "alice" {
			t.Errorf("filters not threaded: %+v", q)
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
		"issue", "list",
		"--state", "closed",
		"--label", "bug",
		"--author", "alice",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestIssueViewJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "title": "answer", "state": "open",
			"user":       map[string]any{"login": "alice"},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-02T00:00:00Z",
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
		"issue", "view", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.Number != 42 || env.Data.Title != "answer" {
		t.Errorf("got %+v", env.Data)
	}
}

func TestIssueViewWithCommentsTriggersTwoCalls(t *testing.T) {
	commentsCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 42, "title": "x", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			})
		case "/repos/o/r/issues/42/comments":
			commentsCalls++
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         1,
					"user":       map[string]any{"login": "bob"},
					"body":       "lgtm",
					"created_at": "2026-04-03T00:00:00Z",
					"updated_at": "2026-04-03T00:00:00Z",
				},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
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
		"issue", "view", "42",
		"--with-comments", "5",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if commentsCalls != 1 {
		t.Errorf("expected 1 comments call; got %d", commentsCalls)
	}
}

func TestIssueViewBadNumber(t *testing.T) {
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
		"issue", "view", "abc",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error on non-numeric arg")
	}
}
