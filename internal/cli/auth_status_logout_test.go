package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

// writeGlobalCred is a small test helper that pre-populates the global
// credentials file under the (test-pinned) HOME, so status/logout
// tests have something to operate on.
func writeGlobalCred(t *testing.T, provider, host string, c auth.Credential) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml")
	store, err := auth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Set(provider, host, c)
	if err := auth.Save(path, store); err != nil {
		t.Fatal(err)
	}
}

func TestAuthStatusEmpty(t *testing.T) {
	clearGaiaEnv(t)

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "no credentials") {
		t.Errorf("output: got %q", stdout.String())
	}
}

func TestAuthStatusJSON(t *testing.T) {
	clearGaiaEnv(t)
	writeGlobalCred(t, "forgejo", "git.example", auth.Credential{Token: "xx", User: "alice"})
	writeGlobalCred(t, "github", "github.com", auth.Credential{Token: "yy", User: "bob"})

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(stdout.String(), "xx") || strings.Contains(stdout.String(), "yy") {
		t.Fatalf("status leaked tokens: %s", stdout.String())
	}
	var env struct {
		Data []struct {
			Provider string `json:"provider"`
			Host     string `json:"host"`
			User     string `json:"user"`
			Source   string `json:"source"`
			TokenSet bool   `json:"token_set"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nstdout: %s", err, stdout.String())
	}
	if len(env.Data) != 2 {
		t.Errorf("entries: got %d, want 2", len(env.Data))
	}
	for _, e := range env.Data {
		if !e.TokenSet {
			t.Errorf("token_set should be true for %+v", e)
		}
		if e.Source != "global" {
			t.Errorf("source: got %q, want global", e.Source)
		}
	}
}

func TestAuthStatusPretty(t *testing.T) {
	clearGaiaEnv(t)
	writeGlobalCred(t, "forgejo", "git.example", auth.Credential{Token: "xx", User: "alice"})

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--format", "pretty", "auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "PROVIDER") {
		t.Errorf("pretty header missing in %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "alice") {
		t.Errorf("user missing in %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "xx") {
		t.Errorf("token leaked: %q", stdout.String())
	}
}

func TestAuthLogoutExactMatch(t *testing.T) {
	clearGaiaEnv(t)
	writeGlobalCred(t, "forgejo", "git.example", auth.Credential{Token: "xx", User: "alice"})

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "logout", "forgejo:git.example"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v\nstderr: %s", err, stderr.String())
	}

	store, err := auth.Load(filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("forgejo", "git.example"); ok {
		t.Errorf("credential still present after logout")
	}
}

func TestAuthLogoutByProviderSingleMatch(t *testing.T) {
	clearGaiaEnv(t)
	writeGlobalCred(t, "forgejo", "git.example", auth.Credential{Token: "xx", User: "alice"})

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "logout", "forgejo"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	store, _ := auth.Load(filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml"))
	if _, ok := store.Get("forgejo", "git.example"); ok {
		t.Errorf("credential still present after logout")
	}
}

func TestAuthLogoutNotFound(t *testing.T) {
	clearGaiaEnv(t)
	writeGlobalCred(t, "forgejo", "git.example", auth.Credential{Token: "xx"})

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "logout", "github"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if got := exitcode.Of(err); got != exitcode.NotFound {
		t.Errorf("exit code: got %d, want NotFound(3)", got)
	}
}

func TestAuthLogoutInteractive(t *testing.T) {
	clearGaiaEnv(t)
	writeGlobalCred(t, "forgejo", "h1", auth.Credential{Token: "x"})
	writeGlobalCred(t, "github", "h2", auth.Credential{Token: "y"})

	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd()
	root.SetIn(strings.NewReader("2\n")) // pick the second
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"auth", "logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	store, _ := auth.Load(filepath.Join(os.Getenv("HOME"), ".config", "gaia", "credentials.yaml"))
	// alphabetical order of Hosts(): forgejo:h1 = #1, github:h2 = #2
	if _, ok := store.Get("github", "h2"); ok {
		t.Errorf("github:h2 should have been removed")
	}
	if _, ok := store.Get("forgejo", "h1"); !ok {
		t.Errorf("forgejo:h1 should still be present")
	}
}
