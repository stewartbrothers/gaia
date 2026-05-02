package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/config"
)

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("Load(missing): got err %v, want nil", err)
	}
	if cfg == nil {
		t.Fatal("Load(missing) should return empty Config, not nil")
	}
	if cfg.DefaultProfile != "" || len(cfg.Profiles) != 0 {
		t.Errorf("Load(missing) should be zero; got %+v", cfg)
	}
}

func TestLoadValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `default_profile: stewartbrothers
profiles:
  stewartbrothers:
    provider: forgejo
    api_url: https://your-forge.example.com/api/v1
    token_env: GIT_FORGE_GITEA_TOKEN
  github:
    provider: github
    api_url: https://api.github.com
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultProfile != "stewartbrothers" {
		t.Errorf("default_profile: got %q", cfg.DefaultProfile)
	}
	sb := cfg.Profiles["stewartbrothers"]
	if sb.Provider != "forgejo" || sb.APIURL != "https://your-forge.example.com/api/v1" || sb.TokenEnv != "GIT_FORGE_GITEA_TOKEN" {
		t.Errorf("stewartbrothers profile: got %+v", sb)
	}
	gh := cfg.Profiles["github"]
	if gh.Provider != "github" || gh.APIURL != "https://api.github.com" {
		t.Errorf("github profile: got %+v", gh)
	}
}

func TestLoadInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error on invalid YAML; got nil")
	}
}

func TestDefaultPathRespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/some-xdg")
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/some-xdg/gaia/config.yaml"
	if got != want {
		t.Errorf("DefaultPath with XDG: got %q, want %q", got, want)
	}
}

func TestDefaultPathFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/test")
	got, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := "/home/test/.config/gaia/config.yaml"
	if got != want {
		t.Errorf("DefaultPath without XDG: got %q, want %q", got, want)
	}
}

func TestProjectPathEmptyRoot(t *testing.T) {
	if got := config.ProjectPath(""); got != "" {
		t.Errorf("empty root must return empty path; got %q", got)
	}
}

func TestProjectPath(t *testing.T) {
	got := config.ProjectPath("/some/repo")
	want := "/some/repo/.gaia/config.yaml"
	if got != want {
		t.Errorf("ProjectPath: got %q, want %q", got, want)
	}
}

// TestMergeProjectShadowsGlobal pins the layering rule: project
// non-empty fields beat global, empty project fields fall through to
// global, and profile maps merge by key.
func TestMergeProjectShadowsGlobal(t *testing.T) {
	global := &config.Config{
		DefaultProfile: "global-default",
		DefaultRepo:    "globalowner/globalrepo",
		Profiles: map[string]config.Profile{
			"sb":   {Provider: "forgejo", APIURL: "https://global.example/api/v1"},
			"only": {Provider: "github", APIURL: "https://api.github.com"},
		},
	}
	project := &config.Config{
		DefaultRepo: "Gerwood/gaia", // project-only override
		Profiles: map[string]config.Profile{
			"sb": {Provider: "forgejo", APIURL: "https://project.example/api/v1"}, // shadows global
		},
	}

	merged := config.Merge(global, project)

	// DefaultProfile not set on project → falls through.
	if merged.DefaultProfile != "global-default" {
		t.Errorf("DefaultProfile: got %q, want global-default", merged.DefaultProfile)
	}
	// DefaultRepo set on project → wins.
	if merged.DefaultRepo != "Gerwood/gaia" {
		t.Errorf("DefaultRepo: got %q, want Gerwood/gaia", merged.DefaultRepo)
	}
	// Project profile shadows global.
	if merged.Profiles["sb"].APIURL != "https://project.example/api/v1" {
		t.Errorf("sb profile: project should win; got %+v", merged.Profiles["sb"])
	}
	// Global-only profile survives.
	if merged.Profiles["only"].Provider != "github" {
		t.Errorf("global-only profile: %+v", merged.Profiles["only"])
	}
}

func TestMergeNilSafe(t *testing.T) {
	if got := config.Merge(nil, nil); got == nil || len(got.Profiles) != 0 {
		t.Errorf("nil/nil should return empty; got %+v", got)
	}
	g := &config.Config{DefaultProfile: "x"}
	if got := config.Merge(g, nil); got.DefaultProfile != "x" {
		t.Errorf("global only: got %+v", got)
	}
	p := &config.Config{DefaultProfile: "y"}
	if got := config.Merge(nil, p); got.DefaultProfile != "y" {
		t.Errorf("project only: got %+v", got)
	}
}
