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

// commentsServer simulates the three Forgejo comment endpoints.
func commentsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         1,
					"user":       map[string]any{"login": "alice"},
					"body":       "first",
					"created_at": "2026-04-01T10:00:00Z",
					"updated_at": "2026-04-01T10:00:00Z",
				},
			})
		case "/repos/o/r/pulls/42/reviews":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":           100,
					"user":         map[string]any{"login": "reviewer"},
					"body":         "lgtm overall",
					"state":        "APPROVED",
					"submitted_at": "2026-04-02T10:00:00Z",
				},
			})
		case "/repos/o/r/pulls/42/comments":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":         200,
					"user":       map[string]any{"login": "reviewer"},
					"body":       "rename this",
					"path":       "core/forgejo/issues.go",
					"line":       42,
					"created_at": "2026-04-03T10:00:00Z",
					"updated_at": "2026-04-03T10:00:00Z",
				},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func TestPRCommentsJSON(t *testing.T) {
	srv := commentsServer(t)
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
		"pr", "comments", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// Comment.Body carries the trust-external tag (#146) so it
	// emits as {"_trust":"external","_value":"…"} on the wire.
	var env struct {
		Data []struct {
			Source string        `json:"source"`
			Body   trustExternal `json:"body"`
			Path   string        `json:"path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 3 {
		t.Fatalf("count: got %d, want 3", len(env.Data))
	}
	wantSources := []string{"issue", "review", "inline"}
	for i, w := range wantSources {
		if env.Data[i].Source != w {
			t.Errorf("[%d]: got %q, want %q", i, env.Data[i].Source, w)
		}
	}
	if env.Data[2].Path != "core/forgejo/issues.go" {
		t.Errorf("inline path missing: %+v", env.Data[2])
	}
}

func TestPRCommentsSourceFilter(t *testing.T) {
	srv := commentsServer(t)
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
		"pr", "comments", "42",
		"--source", "inline",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var env struct {
		Data []struct {
			Source string `json:"source"`
		} `json:"data"`
	}
	_ = json.Unmarshal(stdout.Bytes(), &env)
	if len(env.Data) != 1 || env.Data[0].Source != "inline" {
		t.Errorf("filter result: got %+v", env.Data)
	}
}

func TestPRCommentsPretty(t *testing.T) {
	srv := commentsServer(t)
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
		"pr", "comments", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"[issue] @alice", "[review] @reviewer", "[inline] @reviewer", "core/forgejo/issues.go:42", "lgtm overall", "rename this"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q; got %q", want, out)
		}
	}
}

// TestPRCommentsNDJSON pins the streaming path for the unified
// comment stream (#46): each comment emits as one {"item": ...} line,
// and trust markers persist on every body (Comment.Body carries the
// gaia:"trust=external" tag).
func TestPRCommentsNDJSON(t *testing.T) {
	srv := commentsServer(t)
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
		"--format", "ndjson",
		"pr", "comments", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 3 comments + trailer = 4 lines; got %d:\n%s",
			len(lines), stdout.String())
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(lines[i], `"item"`) {
			t.Errorf("line %d should be item-wrapped; got: %s", i, lines[i])
		}
		if !strings.Contains(lines[i], `"_trust":"external"`) {
			t.Errorf("line %d body should have _trust marker; got: %s", i, lines[i])
		}
	}
	if !strings.Contains(lines[3], `"_metadata"`) {
		t.Errorf("last line should be _metadata trailer; got: %s", lines[3])
	}
}
