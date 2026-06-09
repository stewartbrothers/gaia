package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func fakePRJSON(number int, state string, merged bool) map[string]any {
	pr := map[string]any{
		"number": number, "title": "title", "state": state,
		"user": map[string]any{"login": "alice"},
		"head": map[string]any{
			"ref":  "feature/x",
			"sha":  "deadbeef",
			"repo": map[string]any{"full_name": "o/r"},
		},
		"base": map[string]any{
			"ref":  "main",
			"sha":  "cafebabe",
			"repo": map[string]any{"full_name": "o/r"},
		},
		"merged":     merged,
		"created_at": "2026-04-01T00:00:00Z",
		"updated_at": "2026-04-02T00:00:00Z",
	}
	return pr
}

func TestPRListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			fakePRJSON(1, "open", false),
			fakePRJSON(2, "closed", true),
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
		"pr", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data []struct {
			Number int    `json:"number"`
			State  string `json:"state"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 {
		t.Fatalf("count: %d", len(env.Data))
	}
	if env.Data[1].State != "merged" {
		t.Errorf("state reconciliation: got %q, want merged", env.Data[1].State)
	}
}

func TestPRListPretty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{fakePRJSON(7, "open", false)})
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
		"pr", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"NUMBER", "HEAD", "BASE", "#7", "feature/x", "main"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q; got %q", want, out)
		}
	}
}

func TestPRViewWithCI(t *testing.T) {
	statusCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(fakePRJSON(42, "open", false))
		case "/repos/o/r/commits/deadbeef/status":
			atomic.AddInt32(&statusCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state": "success",
				"statuses": []map[string]any{
					{"state": "success", "context": "build"},
					{"state": "success", "context": "test"},
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
		"pr", "view", "42",
		"--with-ci",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if atomic.LoadInt32(&statusCalls) != 1 {
		t.Errorf("expected one /status call; got %d", statusCalls)
	}
	var env struct {
		Data struct {
			CISummary *struct {
				State      string `json:"state"`
				Total      int    `json:"total"`
				Successful int    `json:"successful"`
			} `json:"ci_summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.CISummary == nil || env.Data.CISummary.State != "success" || env.Data.CISummary.Total != 2 {
		t.Errorf("ci_summary: got %+v", env.Data.CISummary)
	}
}

// fakePRWithCIServer serves a PR plus a two-check commit status
// (build=success, test=failure) for the per-check rendering tests.
func fakePRWithCIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(fakePRJSON(42, "open", false))
		case "/repos/o/r/commits/deadbeef/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state": "failure",
				"statuses": []map[string]any{
					{"status": "success", "context": "CI / Build"},
					{"status": "failure", "context": "CI / Lint"},
				},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
}

// TestPRViewWithCIJSONHasChecks pins that the per-check contexts are
// already carried in the `pr view --with-ci` JSON envelope (the provider
// populates CISummary.Checks on the view path). This is the regression
// guard for the data path; the pretty path is fixed in the next test.
func TestPRViewWithCIJSONHasChecks(t *testing.T) {
	srv := fakePRWithCIServer(t)
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo", "--api-url", srv.URL, "--repo", "o/r",
		"pr", "view", "42", "--with-ci",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var env struct {
		Data struct {
			CISummary *struct {
				Checks []struct {
					Name  string `json:"name"`
					State string `json:"state"`
				} `json:"checks"`
			} `json:"ci_summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.CISummary == nil || len(env.Data.CISummary.Checks) != 2 {
		t.Fatalf("ci_summary.checks: got %+v, want 2 checks", env.Data.CISummary)
	}
	if env.Data.CISummary.Checks[0].Name != "CI / Build" {
		t.Errorf("first check name = %q, want %q", env.Data.CISummary.Checks[0].Name, "CI / Build")
	}
}

// TestPRViewWithCIPrettyListsChecks pins #344: the pretty renderer lists
// each check's context + state under the rollup, not just the aggregate.
func TestPRViewWithCIPrettyListsChecks(t *testing.T) {
	srv := fakePRWithCIServer(t)
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo", "--api-url", srv.URL, "--repo", "o/r",
		"--format", "pretty",
		"pr", "view", "42", "--with-ci",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"CI / Build", "CI / Lint"} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty pr view --with-ci missing check context %q\noutput:\n%s", want, out)
		}
	}
}

func TestPRViewWithoutCISkipsStatusCall(t *testing.T) {
	statusCalls := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/42":
			_ = json.NewEncoder(w).Encode(fakePRJSON(42, "open", false))
		case "/repos/o/r/commits/deadbeef/status":
			atomic.AddInt32(&statusCalls, 1)
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
		"pr", "view", "42",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if atomic.LoadInt32(&statusCalls) != 0 {
		t.Errorf("--with-ci absent must not call /status; got %d calls", statusCalls)
	}
}
