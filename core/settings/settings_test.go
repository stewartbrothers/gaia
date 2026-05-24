package settings_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/settings"
)

// writeFile is a tiny helper for the table-driven cases below.
func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// pinXDG redirects $XDG_CONFIG_HOME at dir so config.DefaultPath
// returns dir/gaia/config.yaml. Restores on cleanup.
func pinXDG(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", dir)
}

// clearTokenEnv unsets every env name the resolver consults so a test
// case's "no token" branch is honest about what's in the environment.
func clearTokenEnv(t *testing.T) {
	t.Helper()
	for _, n := range []string{
		"GAIA_PROFILE", "GAIA_PROVIDER", "FORGEJO_API_URL",
		"FORGEJO_TOKEN", "GITEA_TOKEN", "GITHUB_TOKEN", "GH_TOKEN",
		"GAIA_CACHE_ENABLED",
	} {
		t.Setenv(n, "")
	}
}

func TestLoad_EmptyConfigEmptyOverride(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)

	s, err := settings.Load(settings.Override{Cwd: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := s.Provider(); got != "" {
		t.Errorf("Provider: want empty, got %q", got)
	}
	if got := s.Token(); got != "" {
		t.Errorf("Token: want empty, got %q", got)
	}
	if _, _, ok := s.Repo(); ok {
		t.Errorf("Repo: want ok=false in empty cwd")
	}
	// Cache is enabled by default.
	if !s.Cache().Enabled {
		t.Errorf("Cache.Enabled: default should be true")
	}
}

func TestLoad_GlobalConfig_ProfileDefaults(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)
	t.Setenv("FORGEJO_TOKEN", "tok-from-env")

	writeFile(t, dir, "gaia/config.yaml", `
default_profile: prod
profiles:
  prod:
    provider: forgejo
    api_url: https://forge.example.com/api/v1
`)

	s, err := settings.Load(settings.Override{Cwd: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := s.Profile(), "prod"; got != want {
		t.Errorf("Profile: got %q want %q", got, want)
	}
	if got, want := s.Provider(), "forgejo"; got != want {
		t.Errorf("Provider: got %q want %q", got, want)
	}
	if got, want := s.APIURL(), "https://forge.example.com/api/v1"; got != want {
		t.Errorf("APIURL: got %q want %q", got, want)
	}
	if got, want := s.Token(), "tok-from-env"; got != want {
		t.Errorf("Token: got %q want %q (FORGEJO_TOKEN fallback)", got, want)
	}
}

func TestLoad_FlagOverridesEverything(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)
	t.Setenv("GAIA_PROVIDER", "github")
	t.Setenv("GAIA_PROFILE", "via-env")

	writeFile(t, dir, "gaia/config.yaml", `
default_profile: cfg
profiles:
  cfg:
    provider: forgejo
    api_url: https://cfg.example.com/api/v1
  via-env:
    provider: forgejo
    api_url: https://env.example.com/api/v1
  flag-profile:
    provider: forgejo
    api_url: https://flag.example.com/api/v1
`)

	s, err := settings.Load(settings.Override{
		Cwd:      dir,
		Profile:  "flag-profile",
		Provider: "forgejo",
		APIURL:   "https://override.example.com/api/v1",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := s.Profile(), "flag-profile"; got != want {
		t.Errorf("Profile: got %q want %q (flag wins over env wins over config)", got, want)
	}
	if got, want := s.Provider(), "forgejo"; got != want {
		t.Errorf("Provider: got %q want %q", got, want)
	}
	if got, want := s.APIURL(), "https://override.example.com/api/v1"; got != want {
		t.Errorf("APIURL: got %q want %q (flag wins)", got, want)
	}
}

func TestLoad_PerRequestTokenOverride(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)
	t.Setenv("FORGEJO_TOKEN", "from-env")

	writeFile(t, dir, "gaia/config.yaml", `
default_profile: p
profiles:
  p: { provider: forgejo, api_url: https://x/api/v1 }
`)

	s, err := settings.Load(settings.Override{
		Cwd:   dir,
		Token: "per-request-bearer",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := s.Token(), "per-request-bearer"; got != want {
		t.Errorf("Token: got %q want %q (Override.Token wins)", got, want)
	}
}

func TestLoad_ProjectConfigOverlaysGlobal(t *testing.T) {
	cwd := t.TempDir()
	pinXDG(t, t.TempDir())
	clearTokenEnv(t)
	t.Setenv("FORGEJO_TOKEN", "t")

	// Make cwd a git repo root so ProjectRoot picks it up.
	writeFile(t, cwd, ".git/HEAD", "ref: refs/heads/main\n")
	writeFile(t, cwd, ".gaia/config.yaml", `
default_profile: proj
default_repo: org/proj-repo
profiles:
  proj:
    provider: forgejo
    api_url: https://proj.example.com/api/v1
`)

	s, err := settings.Load(settings.Override{Cwd: cwd})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := s.Profile(), "proj"; got != want {
		t.Errorf("Profile: got %q want %q (project layer wins)", got, want)
	}
	if got, want := s.DefaultRepo(), "org/proj-repo"; got != want {
		t.Errorf("DefaultRepo: got %q want %q", got, want)
	}
	owner, name, ok := s.Repo()
	if !ok || owner != "org" || name != "proj-repo" {
		t.Errorf("Repo: got (%q,%q,%v) want (org,proj-repo,true)", owner, name, ok)
	}
}

func TestLoad_RepoFlagWinsOverProjectDefault(t *testing.T) {
	cwd := t.TempDir()
	pinXDG(t, t.TempDir())
	clearTokenEnv(t)
	t.Setenv("FORGEJO_TOKEN", "t")

	writeFile(t, cwd, ".git/HEAD", "ref: refs/heads/main\n")
	writeFile(t, cwd, ".gaia/config.yaml", `
default_profile: p
default_repo: org/proj-default
profiles:
  p: { provider: forgejo, api_url: https://x/api/v1 }
`)

	s, err := settings.Load(settings.Override{
		Cwd:  cwd,
		Repo: "flag/repo",
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	owner, name, ok := s.Repo()
	if !ok || owner != "flag" || name != "repo" {
		t.Errorf("Repo: got (%q,%q,%v) want (flag,repo,true)", owner, name, ok)
	}
}

func TestLoad_CacheDisabledByEnv(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)
	t.Setenv("GAIA_CACHE_ENABLED", "false")

	s, err := settings.Load(settings.Override{Cwd: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Cache().NoCache {
		t.Errorf("Cache.NoCache: want true under GAIA_CACHE_ENABLED=false")
	}
}

func TestLoad_NoCacheFlagOverridesConfig(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)

	writeFile(t, dir, "gaia/config.yaml", `
cache:
  enabled: true
`)

	s, err := settings.Load(settings.Override{
		Cwd:     dir,
		NoCache: true,
	})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.Cache().NoCache {
		t.Errorf("Cache.NoCache: want true under --no-cache flag")
	}
	if !s.Cache().Enabled {
		t.Errorf("Cache.Enabled: want true (config says so); NoCache is the per-invocation bypass")
	}
}

func TestLoad_MissingProfileNamedByFlag_Errors(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)

	writeFile(t, dir, "gaia/config.yaml", `
profiles:
  real: { provider: forgejo, api_url: https://x/api/v1 }
`)

	_, err := settings.Load(settings.Override{
		Cwd:     dir,
		Profile: "does-not-exist",
	})
	if err == nil {
		t.Fatalf("Load: want error for missing profile, got nil")
	}
}

func TestLoad_EagerEvaluation_InspectorPointersStable(t *testing.T) {
	// Eager evaluation contract: after Load returns, Inspector reads
	// pre-computed values. Calling Inspector().GlobalConfig() twice
	// must return the same pointer (no re-load).
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)
	writeFile(t, dir, "gaia/config.yaml", `default_profile: p
profiles:
  p: { provider: forgejo, api_url: https://x/api/v1 }`)

	s, err := settings.Load(settings.Override{Cwd: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	insp := s.Inspector()
	a := insp.GlobalConfig()
	b := insp.GlobalConfig()
	if a != b {
		t.Errorf("Inspector.GlobalConfig: pointer identity must be stable across calls (re-load detected)")
	}
}

func TestContext_WithSettings_FromContext(t *testing.T) {
	dir := t.TempDir()
	pinXDG(t, dir)
	clearTokenEnv(t)

	s, err := settings.Load(settings.Override{Cwd: dir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx := settings.WithSettings(context.Background(), s)
	got, ok := settings.FromContext(ctx)
	if !ok {
		t.Fatalf("FromContext: want ok, got false")
	}
	if got != s {
		t.Errorf("FromContext: returned different Settings than stashed")
	}
}

func TestFromContext_Empty(t *testing.T) {
	_, ok := settings.FromContext(context.Background())
	if ok {
		t.Errorf("FromContext: empty context should return ok=false")
	}
}
