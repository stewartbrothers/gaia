package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// TestIssueDepListBlockers pins the default direction: `gaia issue
// dep list 42` hits .../42/dependencies.
func TestIssueDepListBlockers(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/42/dependencies" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		hits++
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number": 7, "title": "blocker", "state": "open",
				"user":       map[string]any{"login": "alice"},
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
		"issue", "dep", "list", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if hits != 1 {
		t.Errorf("expected 1 GET; got %d", hits)
	}
}

// TestIssueDepListBlocking pins the explicit --direction blocks
// flag: hits .../42/blocks.
func TestIssueDepListBlocking(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
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
		"issue", "dep", "list", "42",
		"--direction", "blocks",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if path != "/repos/o/r/issues/42/blocks" {
		t.Errorf("expected /blocks endpoint; got %q", path)
	}
}

func TestIssueDepListBadDirection(t *testing.T) {
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
		"issue", "dep", "list", "42",
		"--direction", "sideways",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected usage error for bad --direction")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}

// TestIssueDepAddBlocker pins the "M blocks N" framing — POSTs to
// N's /dependencies with body {"index": M}.
func TestIssueDepAddBlocker(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s", r.Method)
		}
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 7, "title": "added blocker", "state": "open",
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
		"issue", "dep", "add", "42",
		"--blocker", "7",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if gotPath != "/repos/o/r/issues/42/dependencies" {
		t.Errorf("path: got %q, want /repos/o/r/issues/42/dependencies", gotPath)
	}
	if int(gotBody["index"].(float64)) != 7 {
		t.Errorf("body index: got %v, want 7", gotBody["index"])
	}
}

// TestIssueDepAddBlocks pins the inverse framing: --blocks M on
// argument N means "N blocks M" → edge stored on M's /dependencies
// with body {"index": N}. Symmetric: same edge, opposite direction
// of the argument flow.
func TestIssueDepAddBlocks(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "title": "added", "state": "open",
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
		"issue", "dep", "add", "42",
		"--blocks", "7",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// "42 blocks 7" → edge on 7's /dependencies, body says blocker=42.
	if gotPath != "/repos/o/r/issues/7/dependencies" {
		t.Errorf("path: got %q, want /repos/o/r/issues/7/dependencies", gotPath)
	}
	if int(gotBody["index"].(float64)) != 42 {
		t.Errorf("body index: got %v, want 42", gotBody["index"])
	}
}

func TestIssueDepAddMutuallyExclusive(t *testing.T) {
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
		"issue", "dep", "add", "42",
		"--blocker", "7",
		"--blocks", "8",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected usage error when both --blocker and --blocks supplied")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}

func TestIssueDepAddRequiresOneFlag(t *testing.T) {
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
		"issue", "dep", "add", "42",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected usage error when neither flag supplied")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}

func TestIssueDepRemoveBlocker(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotBody   map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{})
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
		"issue", "dep", "remove", "42",
		"--blocker", "7",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: got %s, want DELETE", gotMethod)
	}
	if gotPath != "/repos/o/r/issues/42/dependencies" {
		t.Errorf("path: got %q", gotPath)
	}
	if int(gotBody["index"].(float64)) != 7 {
		t.Errorf("body index: got %v, want 7", gotBody["index"])
	}
}

// TestIssueViewWithBlockersTriggersExtraCall pins the inline-fetch
// path: --with-blockers N causes GetIssue to also call
// /dependencies, and the response carries the blockers field.
func TestIssueViewWithBlockersTriggersExtraCall(t *testing.T) {
	var depsCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 42, "title": "thing", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			})
		case "/repos/o/r/issues/42/dependencies":
			depsCalls++
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number": 7, "title": "blocker", "state": "open",
					"user":       map[string]any{"login": "alice"},
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-02T00:00:00Z",
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
		"--with-blockers", "5",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if depsCalls != 1 {
		t.Errorf("expected 1 /dependencies call; got %d", depsCalls)
	}
	if !strings.Contains(stdout.String(), `"blockers"`) {
		t.Errorf("expected blockers field in output; got %q", stdout.String())
	}
}

// TestIssueViewPrettyRendersBlockersSection pins #323: the pretty
// formatter renders inlined blockers under a "--- Blockers ---"
// section, with each entry header + fenced title.
func TestIssueViewPrettyRendersBlockersSection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 42, "title": "the host issue", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			})
		case "/repos/o/r/issues/42/dependencies":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number": 7, "title": "first blocker", "state": "open",
					"user":       map[string]any{"login": "alice"},
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-02T00:00:00Z",
				},
				{
					"number": 8, "title": "second blocker", "state": "closed",
					"user":       map[string]any{"login": "bob"},
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-02T00:00:00Z",
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
		"--format", "pretty",
		"issue", "view", "42",
		"--with-blockers", "5",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "--- Blockers") {
		t.Errorf("missing Blockers section header; got:\n%s", out)
	}
	if !strings.Contains(out, "#7 (open):") || !strings.Contains(out, "#8 (closed):") {
		t.Errorf("missing per-blocker header lines; got:\n%s", out)
	}
	// Titles must be fenced (forge-supplied via #146).
	if !strings.Contains(out, "first blocker") || !strings.Contains(out, "<<<EXTERNAL") {
		t.Errorf("expected fenced title; got:\n%s", out)
	}
	// Blocking section absent (no --with-blocking flag).
	if strings.Contains(out, "--- Blocking") {
		t.Errorf("Blocking section should NOT appear without --with-blocking; got:\n%s", out)
	}
}

// TestIssueViewPrettyRendersBlockingSection pins the inverse — the
// Blocking section renders when --with-blocking is set, and the
// Blockers section is absent when its flag isn't.
func TestIssueViewPrettyRendersBlockingSection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "upstream", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			})
		case "/repos/o/r/issues/7/blocks":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number": 100, "title": "downstream", "state": "open",
					"user":       map[string]any{"login": "alice"},
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-02T00:00:00Z",
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
		"--format", "pretty",
		"issue", "view", "7",
		"--with-blocking", "5",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "--- Blocking") {
		t.Errorf("missing Blocking section header; got:\n%s", out)
	}
	if !strings.Contains(out, "#100 (open):") {
		t.Errorf("missing per-blocking header line; got:\n%s", out)
	}
	if strings.Contains(out, "--- Blockers") {
		t.Errorf("Blockers section should NOT appear without --with-blockers; got:\n%s", out)
	}
}

// TestIssueViewWithBlockingTriggersExtraCall pins the inverse:
// --with-blocking N hits /blocks and populates the blocks field.
func TestIssueViewWithBlockingTriggersExtraCall(t *testing.T) {
	var blocksCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/issues/42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 42, "title": "thing", "state": "open",
				"user":       map[string]any{"login": "alice"},
				"created_at": "2026-04-01T00:00:00Z",
				"updated_at": "2026-04-02T00:00:00Z",
			})
		case "/repos/o/r/issues/42/blocks":
			blocksCalls++
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number": 100, "title": "downstream", "state": "open",
					"user":       map[string]any{"login": "alice"},
					"created_at": "2026-04-01T00:00:00Z",
					"updated_at": "2026-04-02T00:00:00Z",
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
		"--with-blocking", "5",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if blocksCalls != 1 {
		t.Errorf("expected 1 /blocks call; got %d", blocksCalls)
	}
	if !strings.Contains(stdout.String(), `"blocks"`) {
		t.Errorf("expected blocks field in output; got %q", stdout.String())
	}
}

// TestIssueDepAddCrossRepoBlocker pins #325 at the CLI level:
// --blocker owner/repo#7 on a host issue parses the cross-repo
// reference and threads owner+repo into the upstream body.
func TestIssueDepAddCrossRepoBlocker(t *testing.T) {
	var capturedBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 7, "title": "cross-repo blocker", "state": "open",
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
		"--repo", "host-owner/host-repo",
		"issue", "dep", "add", "42",
		"--blocker", "other-owner/other-repo#7",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// POST hits the HOST's /dependencies endpoint (host = the
	// argument issue, lives in --repo).
	if gotPath != "/repos/host-owner/host-repo/issues/42/dependencies" {
		t.Errorf("path: got %q", gotPath)
	}
	if int(capturedBody["index"].(float64)) != 7 {
		t.Errorf("body index: got %v", capturedBody["index"])
	}
	if capturedBody["owner"] != "other-owner" {
		t.Errorf("body owner: got %v", capturedBody["owner"])
	}
	if capturedBody["repo"] != "other-repo" {
		t.Errorf("body repo: got %v", capturedBody["repo"])
	}
}

// TestIssueDepAddBlocksCrossRepoFlipsHost pins the inverse framing
// with a cross-repo target: --blocks other-owner/other-repo#7 on
// issue 42 means "42 (in --repo) blocks 7 (in other-owner/other-
// repo)." Edge lives on the BLOCKED side (the flag's repo), so the
// POST hits other-owner/other-repo's /dependencies path with the
// body pointing back at --repo#42.
func TestIssueDepAddBlocksCrossRepoFlipsHost(t *testing.T) {
	var capturedBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 42, "title": "added", "state": "open",
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
		"--repo", "host-owner/host-repo",
		"issue", "dep", "add", "42",
		"--blocks", "other-owner/other-repo#7",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	// Edge stored on the BLOCKED side's repo (= --blocks's flag repo).
	if gotPath != "/repos/other-owner/other-repo/issues/7/dependencies" {
		t.Errorf("path: got %q, want /repos/other-owner/other-repo/issues/7/dependencies", gotPath)
	}
	if int(capturedBody["index"].(float64)) != 42 {
		t.Errorf("body index: got %v, want 42", capturedBody["index"])
	}
	if capturedBody["owner"] != "host-owner" {
		t.Errorf("body owner: got %v", capturedBody["owner"])
	}
	if capturedBody["repo"] != "host-repo" {
		t.Errorf("body repo: got %v", capturedBody["repo"])
	}
}

// TestIssueDepAddCrossRepoMalformed pins the parse-error path —
// invalid owner/repo#N input surfaces as a Usage exit code.
func TestIssueDepAddCrossRepoMalformed(t *testing.T) {
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
		"issue", "dep", "add", "42",
		"--blocker", "not-a-valid-ref",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected usage error for malformed dep ref")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}
