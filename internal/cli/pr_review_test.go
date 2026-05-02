package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestPRReviewApprove(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
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
		"pr", "review", "42",
		"--state", "approve",
		"--body", "ship it",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	if got["event"] != "APPROVED" || got["body"] != "ship it" {
		t.Errorf("body: %+v", got)
	}
}

func TestPRReviewRequestChangesWithInline(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(200)
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
		"pr", "review", "42",
		"--state", "request-changes",
		"--body", "see inline",
		"--comment", "foo.go:10:rename this",
		"--comment", "bar.go:20:tighten loop",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var got map[string]any
	_ = json.Unmarshal(captured, &got)
	if got["event"] != "REQUEST_CHANGES" {
		t.Errorf("event: %+v", got)
	}
	comments, _ := got["comments"].([]any)
	if len(comments) != 2 {
		t.Fatalf("comments count: %d", len(comments))
	}
	first := comments[0].(map[string]any)
	if first["path"] != "foo.go" || first["new_position"].(float64) != 10 || first["body"] != "rename this" {
		t.Errorf("first: %+v", first)
	}
}

func TestPRReviewBadStateRejected(t *testing.T) {
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
		"pr", "review", "42",
		"--state", "wat",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on unknown state")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("got %d, want Usage", got)
	}
}

func TestPRReviewBadCommentFormatRejected(t *testing.T) {
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
		"pr", "review", "42",
		"--state", "approve",
		"--comment", "missing-line-and-body",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on malformed --comment")
	}
}

func TestPRReviewDryRun(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
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
		"pr", "review", "42",
		"--state", "approve",
		"--body", "ok",
		"--dry-run",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 0 {
		t.Errorf("--dry-run must not POST; got %d calls", calls)
	}
	if !strings.Contains(stdout.String(), `"event": "APPROVED"`) {
		t.Errorf("dry-run output: %q", stdout.String())
	}
}
