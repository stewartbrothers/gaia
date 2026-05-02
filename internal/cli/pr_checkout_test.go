package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

// recordingGitRunner captures every git invocation the checkout
// subcommand would have made. Tests assert on the captured args
// rather than running real git.
type recordingGitRunner struct {
	calls [][]string
}

func (r *recordingGitRunner) run(_ context.Context, _ string, args ...string) error {
	r.calls = append(r.calls, append([]string(nil), args...))
	return nil
}

func TestPRCheckoutFetchAndCheckout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     42,
			"state":      "open",
			"user":       map[string]any{"login": "alice"},
			"head":       map[string]any{"ref": "feature/x", "sha": "deadbeef", "repo": map[string]any{"full_name": "o/r"}},
			"base":       map[string]any{"ref": "main", "sha": "cafebabe", "repo": map[string]any{"full_name": "o/r"}},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-02T00:00:00Z",
		})
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	rec := &recordingGitRunner{}
	cli.SetGitRunnerForTest(rec.run)
	defer cli.SetGitRunnerForTest(nil)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"pr", "checkout", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	if len(rec.calls) != 2 {
		t.Fatalf("calls: got %d, want 2 (fetch + checkout)", len(rec.calls))
	}
	if got := strings.Join(rec.calls[0], " "); got != "fetch origin refs/pull/42/head:pr-42" {
		t.Errorf("first call: %q", got)
	}
	if got := strings.Join(rec.calls[1], " "); got != "checkout pr-42" {
		t.Errorf("second call: %q", got)
	}
	if !strings.Contains(stdout.String(), "Checked out PR #42 on branch pr-42") {
		t.Errorf("stdout: %q", stdout.String())
	}
}

func TestPRCheckoutDetach(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number":     7,
			"state":      "open",
			"user":       map[string]any{"login": "alice"},
			"head":       map[string]any{"ref": "feat", "sha": "abc123", "repo": map[string]any{"full_name": "o/r"}},
			"base":       map[string]any{"ref": "main", "sha": "0", "repo": map[string]any{"full_name": "o/r"}},
			"created_at": "2026-04-01T00:00:00Z",
			"updated_at": "2026-04-02T00:00:00Z",
		})
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	rec := &recordingGitRunner{}
	cli.SetGitRunnerForTest(rec.run)
	defer cli.SetGitRunnerForTest(nil)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--repo", "o/r",
		"pr", "checkout", "7",
		"--detach",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(rec.calls) != 2 {
		t.Fatalf("calls: got %d, want 2", len(rec.calls))
	}
	if got := strings.Join(rec.calls[1], " "); got != "checkout --detach abc123" {
		t.Errorf("checkout call: %q", got)
	}
	if !strings.Contains(stdout.String(), "(detached)") {
		t.Errorf("stdout should mention detached; got %q", stdout.String())
	}
}
