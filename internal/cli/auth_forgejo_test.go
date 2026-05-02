package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestAuthForgejoHappyPath(t *testing.T) {
	clearGaiaEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" {
			t.Errorf("path: got %q, want /api/v1/user", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token PASTED" {
			t.Errorf("token: got %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "Gerwood"})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("PASTED\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "forgejo", srv.URL})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Gerwood") {
		t.Errorf("output should mention login Gerwood; got %q", out)
	}

	// Credential should be saved at the global path under HOME (clearGaiaEnv pins HOME to a tempdir).
	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	store, err := auth.Load(credPath)
	if err != nil {
		t.Fatalf("Load credentials: %v", err)
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	c, ok := store.Get("forgejo", host)
	if !ok {
		t.Fatalf("expected credential for forgejo:%s; got hosts=%v", host, store.Hosts())
	}
	if c.Token != "PASTED" || c.User != "Gerwood" {
		t.Errorf("credential: got %+v", c)
	}
	if !strings.HasSuffix(c.APIURL, "/api/v1") {
		t.Errorf("APIURL should end with /api/v1; got %q", c.APIURL)
	}
}

func TestAuthForgejoURLNormalization(t *testing.T) {
	cases := []string{
		"https://git.example",
		"https://git.example/",
		"https://git.example/api/v1",
		"https://git.example/api/v1/",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			normalized := cli.NormalizeForgejoURLForTest(raw)
			if !strings.HasSuffix(normalized, "/api/v1") {
				t.Errorf("normalize(%q): got %q, want suffix /api/v1", raw, normalized)
			}
			if strings.Contains(normalized, "/api/v1/api/v1") {
				t.Errorf("normalize(%q): doubled suffix: %q", raw, normalized)
			}
		})
	}
}

func TestAuthForgejoInvalidTokenIsNotPersisted(t *testing.T) {
	clearGaiaEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("BAD\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "forgejo", srv.URL})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth(4)", got)
	}

	// The credential file must NOT exist (we didn't validate, so we don't persist).
	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	if _, err := os.Stat(credPath); err == nil {
		t.Errorf("credentials file should not exist after invalid-token: %s", credPath)
	}
}

func TestAuthForgejoEmptyTokenRejected(t *testing.T) {
	clearGaiaEnv(t)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "forgejo", "https://git.example"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on empty token")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}

func TestAuthForgejoProjectFlagWritesToProjectAndGitignores(t *testing.T) {
	clearGaiaEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "Gerwood"})
	}))
	defer srv.Close()

	// Set up a fake repo: a directory with a .git dir.
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("PASTED\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "forgejo", srv.URL, "--project"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	credPath := filepath.Join(repo, ".gaia", "credentials.yaml")
	if _, err := os.Stat(credPath); err != nil {
		t.Fatalf("project credentials should exist: %v", err)
	}
	gi, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatalf("gitignore should exist: %v", err)
	}
	if !strings.Contains(string(gi), ".gaia/credentials.yaml") {
		t.Errorf(".gitignore should list credentials.yaml; got %q", string(gi))
	}
}

func TestBuildProviderUsesCredentialsWhenNoEnv(t *testing.T) {
	// MVP-1 dogfood checkpoint: after `gaia auth forgejo`, `gaia whoami` works
	// with no env vars. This test simulates the post-auth state by writing a
	// credential and asserting whoami reads it.
	clearGaiaEnv(t)

	// Httptest server simulates Forgejo. /api/v1/user returns Gerwood.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "Gerwood"})
	}))
	defer srv.Close()

	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	store := &auth.Store{}
	host := strings.TrimPrefix(srv.URL, "http://")
	store.Set("forgejo", host, auth.Credential{
		APIURL: srv.URL + "/api/v1",
		Token:  "STORED",
		User:   "Gerwood",
	})
	if err := auth.Save(credPath, store); err != nil {
		t.Fatal(err)
	}

	// Now run `gaia whoami` with NO --provider, NO --api-url, NO env. It
	// should pick the one stored credential and use it.
	// Note: the httptest server above won't see the call because gaia
	// will use the APIURL on the credential, which points to srv. So
	// the same server is hit.
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"whoami"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	var env struct {
		Data struct {
			Login string `json:"login"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.Login != "Gerwood" {
		t.Errorf("login: got %q, want Gerwood", env.Data.Login)
	}
}
