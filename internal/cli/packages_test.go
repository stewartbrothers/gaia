package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestPackagesListJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages/o" {
			t.Errorf("path: %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"type":       "generic",
				"name":       "alpha",
				"version":    "1.0.0",
				"owner":      map[string]any{"login": "o"},
				"created_at": "2026-04-01T00:00:00Z",
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
		"packages", "list",
		"--owner", "o",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data []struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Version string `json:"version"`
			Owner   string `json:"owner"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout.String())
	}
	if len(env.Data) != 1 || env.Data[0].Name != "alpha" {
		t.Errorf("data: %+v", env.Data)
	}
}

func TestPackagesListWithType(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
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
		"packages", "list",
		"--owner", "o",
		"--type", "npm",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(capturedQuery, "type=npm") {
		t.Errorf("type filter not in query: %q", capturedQuery)
	}
}

func TestPackagesViewRequiresSpec(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", "http://x",
		"packages", "view",
		"--owner", "o",
		"bad-spec", // missing slashes
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for malformed spec")
	}
}

func TestPackagesView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/packages/o/generic/alpha/1.0.0"
		if r.URL.Path != want {
			t.Errorf("path: got %q want %q", r.URL.Path, want)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":       "generic",
			"name":       "alpha",
			"version":    "1.0.0",
			"owner":      map[string]any{"login": "o"},
			"created_at": "2026-04-01T00:00:00Z",
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
		"packages", "view",
		"--owner", "o",
		"generic/alpha/1.0.0",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Type    string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.Name != "alpha" || env.Data.Version != "1.0.0" || env.Data.Type != "generic" {
		t.Errorf("data: %+v", env.Data)
	}
}

func TestPackagesDeleteWithoutConfirmIsPreview(t *testing.T) {
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
		"packages", "delete",
		"--owner", "o",
		"generic/alpha/1.0.0",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls != 0 {
		t.Errorf("preview must not call API; got %d calls", calls)
	}
	if !strings.Contains(stdout.String(), "Would delete") {
		t.Errorf("stdout: %q", stdout.String())
	}
}

func TestPackagesDeleteWithConfirm(t *testing.T) {
	deletePath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletePath = r.URL.Path
			w.WriteHeader(204)
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
		"packages", "delete",
		"--owner", "o",
		"generic/alpha/1.0.0",
		"--confirm",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if deletePath != "/packages/o/generic/alpha/1.0.0" {
		t.Errorf("DELETE path: got %q", deletePath)
	}
}

// TestPackagesOwnerFallsBackToRepoOwner: --owner is optional; when
// omitted we reuse the auto-detect path that other commands use
// (--repo / project default / git remote). Verifies that --repo
// drives the owner when --owner isn't passed.
func TestPackagesOwnerFallsBackToRepoOwner(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
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
		"--repo", "fallback-owner/some-repo",
		"packages", "list",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if capturedPath != "/packages/fallback-owner" {
		t.Errorf("path: got %q, want /packages/fallback-owner", capturedPath)
	}
}

// TestPackagesNeedsOwnerOrRepo asserts the error message when neither
// --owner nor --repo is set and there's no autodetect to fall back
// to.
func TestPackagesNeedsOwnerOrRepo(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", "http://x",
		"packages", "list",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when neither --owner nor --repo is set")
	}
}

// TestPackagesUploadFromFile drives `gaia packages upload` with a
// real on-disk artifact. Verifies the PUT lands on the generic-
// package endpoint with the expected path + body.
func TestPackagesUploadFromFile(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "myapp.tar.gz")
	if err := os.WriteFile(artifact, []byte("HELLO-PAYLOAD"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	var (
		gotMethod      string
		gotPath        string
		gotContentType string
		gotBody        []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
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
		"packages", "upload",
		"--owner", "o",
		"--content-type", "application/gzip",
		"generic", "myapp", "1.2.0", artifact,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method: got %q", gotMethod)
	}
	if gotPath != "/packages/o/generic/myapp/1.2.0/myapp.tar.gz" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotContentType != "application/gzip" {
		t.Errorf("content-type: got %q", gotContentType)
	}
	if string(gotBody) != "HELLO-PAYLOAD" {
		t.Errorf("body: got %q", string(gotBody))
	}
	if !strings.Contains(stdout.String(), "✓ Uploaded") {
		t.Errorf("stdout: %q", stdout.String())
	}
}

// TestPackagesUploadFromStdin streams the artifact body from stdin
// when <file> = "-". --filename is required because there's no path
// to derive a basename from.
func TestPackagesUploadFromStdin(t *testing.T) {
	var gotBody []byte
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(201)
	}))
	defer srv.Close()
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader("STDIN-PAYLOAD"))
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"packages", "upload",
		"--owner", "o",
		"--filename", "stream.bin",
		"generic", "myapp", "1.0.0", "-",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if gotPath != "/packages/o/generic/myapp/1.0.0/stream.bin" {
		t.Errorf("path: got %q", gotPath)
	}
	if string(gotBody) != "STDIN-PAYLOAD" {
		t.Errorf("body: got %q", string(gotBody))
	}
}

// TestPackagesUploadStdinRequiresFileName: --filename is required for
// stdin uploads since there's no path to derive a basename from.
func TestPackagesUploadStdinRequiresFileName(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader("data"))
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", "http://x",
		"packages", "upload",
		"--owner", "o",
		"generic", "myapp", "1.0", "-",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error when --filename omitted with stdin")
	}
}

// TestPackagesUploadFileNotFound surfaces a file-open error rather
// than a network call.
func TestPackagesUploadFileNotFound(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "X")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", "http://x",
		"packages", "upload",
		"--owner", "o",
		"generic", "myapp", "1.0", "/nonexistent/path/that/does/not/exist",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestPackagesUploadOverridesFilename: --filename overrides the
// basename-derived default.
func TestPackagesUploadOverridesFilename(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "build.tgz")
	if err := os.WriteFile(artifact, []byte("X"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(201)
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
		"packages", "upload",
		"--owner", "o",
		"--filename", "renamed.tar.gz",
		"generic", "myapp", "1.0", artifact,
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(gotPath, "renamed.tar.gz") {
		t.Errorf("path should use overridden filename; got %q", gotPath)
	}
}
