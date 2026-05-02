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

func clearGaiaEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GAIA_PROFILE", "GAIA_PROVIDER",
		"FORGEJO_TOKEN", "FORGEJO_API_URL",
		"GITHUB_TOKEN", "GIT_FORGE_GITEA_TOKEN",
		"XDG_CONFIG_HOME",
	} {
		t.Setenv(k, "")
	}
	// Pin HOME to a directory with no gaia config so config.Load is a
	// no-op rather than reading the dev's actual ~/.config/gaia.
	t.Setenv("HOME", t.TempDir())
}

func TestWhoamiJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"login":      "Gerwood",
			"avatar_url": "https://x",
		})
	}))
	defer srv.Close()

	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "TEST")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"whoami",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	var env struct {
		Data struct {
			Login    string `json:"login"`
			Provider string `json:"provider"`
			Host     string `json:"host"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if env.Data.Login != "Gerwood" {
		t.Errorf("login: got %q", env.Data.Login)
	}
	if env.Data.Provider != "forgejo" {
		t.Errorf("provider: got %q", env.Data.Provider)
	}
	if env.Data.Host == "" {
		t.Errorf("host should be set")
	}
}

func TestWhoamiPretty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "Gerwood"})
	}))
	defer srv.Close()

	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "TEST")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"--format", "pretty",
		"whoami",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "Gerwood" {
		t.Errorf("pretty output: got %q, want Gerwood", stdout.String())
	}
}

func TestWhoamiAuthErrorMapsToExitCode4(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "BAD")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "forgejo",
		"--api-url", srv.URL,
		"whoami",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if got := exitcode.Of(err); got != exitcode.Auth {
		t.Errorf("exit code: got %d, want Auth(4)", got)
	}
}

func TestWhoamiNoProviderConfiguredIsUsageError(t *testing.T) {
	clearGaiaEnv(t)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"whoami"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when no provider configured")
	}
	if got := exitcode.Of(err); got != exitcode.Usage {
		t.Errorf("exit code: got %d, want Usage(2)", got)
	}
}

func TestWhoamiGitHubProviderNotImplemented(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("GITHUB_TOKEN", "TEST")

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--provider", "github",
		"--api-url", "https://api.github.com",
		"whoami",
	})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unimplemented github provider")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Errorf("error should mention github; got %q", err.Error())
	}
}
