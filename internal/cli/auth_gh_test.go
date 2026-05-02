package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

func TestAuthGHHappyPath(t *testing.T) {
	clearGaiaEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer GHTOKEN" {
			t.Errorf("Authorization: got %q, want %q", got, "Bearer GHTOKEN")
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got == "" {
			t.Errorf("X-GitHub-Api-Version header missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "gerwood"})
	}))
	defer srv.Close()

	prev := cli.SetGithubAPIURLForTest(srv.URL)
	defer cli.SetGithubAPIURLForTest(prev)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("GHTOKEN\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "gh"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "gerwood") {
		t.Errorf("output should mention login; got %q", stdout.String())
	}

	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	store, err := auth.Load(credPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	parsed, _ := url.Parse(srv.URL)
	c, ok := store.Get("github", parsed.Host)
	if !ok || c.Token != "GHTOKEN" || c.User != "gerwood" || c.APIURL != srv.URL {
		t.Errorf("credential: got %+v ok=%v", c, ok)
	}
}

func TestAuthGHInvalidTokenNotPersisted(t *testing.T) {
	clearGaiaEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	prev := cli.SetGithubAPIURLForTest(srv.URL)
	defer cli.SetGithubAPIURLForTest(prev)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("BAD\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "gh"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth(4)", got)
	}

	credPath := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	if _, err := os.Stat(credPath); err == nil {
		t.Errorf("credentials file should not exist after invalid token")
	}
}

func TestAuthGHEmptyTokenRejected(t *testing.T) {
	clearGaiaEnv(t)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "gh"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}
