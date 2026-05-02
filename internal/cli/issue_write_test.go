package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestIssueCreateDryRunDoesNotPost(t *testing.T) {
	calls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
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
		"issue", "create",
		"--title", "test",
		"--body", "body",
		"--dry-run",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("--dry-run must not POST; got %d calls", calls)
	}
	if !strings.Contains(stdout.String(), `"title": "test"`) {
		t.Errorf("dry-run should print body with snake_case json tags; got %q", stdout.String())
	}
}

func TestIssueCreatePostsAndReturnsIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: %q", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"title":"hello"`) {
			t.Errorf("body: %s", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     42,
			"title":      "hello",
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
		"issue", "create",
		"--title", "hello",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data struct {
			Number int `json:"number"`
		} `json:"data"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &env)
	if env.Data.Number != 42 {
		t.Errorf("expected number 42; got %+v", env.Data)
	}
}

func TestIssueCreateRequiresTitle(t *testing.T) {
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
		"issue", "create",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when --title is missing")
	}
}

func TestIssueCloseSendsState(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method: %q", r.Method)
		}
		capturedBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     86,
			"title":      "x",
			"state":      "closed",
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
		"issue", "close", "86",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(string(capturedBody), `"state":"closed"`) {
		t.Errorf("body: %s", capturedBody)
	}
}

func TestIssueCommentReadsStdin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["body"] != "from stdin" {
			t.Errorf("body: %+v", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         9,
			"user":       map[string]any{"login": "alice"},
			"body":       "from stdin",
			"created_at": "2026-05-01T00:00:00Z",
			"updated_at": "2026-05-01T00:00:00Z",
		})
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("from stdin"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"issue", "comment", "42",
		"--body", "-",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
}

func TestIssueCommentDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method: %q", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/issues/comments/9") {
			t.Errorf("path: %q", r.URL.Path)
		}
		w.WriteHeader(204)
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
		"issue", "comment-delete", "9",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "Deleted comment 9") {
		t.Errorf("output: %q", stdout.String())
	}
}
