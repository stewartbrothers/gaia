package config_test

import (
	"strings"
	"testing"

	"github.com/stewartbrothers/gaia/core/config"
)

func clearGaiaEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GAIA_PROFILE", "GAIA_PROVIDER",
		"FORGEJO_TOKEN", "FORGEJO_API_URL",
		"GITHUB_TOKEN",
		"GIT_FORGE_GITEA_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

func TestResolveNoConfigUsesEnvOnly(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("GAIA_PROVIDER", "forgejo")
	t.Setenv("FORGEJO_TOKEN", "secret-token")
	t.Setenv("FORGEJO_API_URL", "https://forge.example/api/v1")

	got, err := config.Resolve(nil, config.Override{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Provider != "forgejo" || got.APIURL != "https://forge.example/api/v1" || got.Token != "secret-token" {
		t.Errorf("got %+v", got)
	}
}

func TestResolveFlagOverridesEnv(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("GAIA_PROVIDER", "github")
	t.Setenv("FORGEJO_API_URL", "https://env.example/api/v1")

	got, err := config.Resolve(nil, config.Override{
		Provider: "forgejo",
		APIURL:   "https://flag.example/api/v1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Provider != "forgejo" {
		t.Errorf("provider: got %q, want forgejo (flag wins)", got.Provider)
	}
	if got.APIURL != "https://flag.example/api/v1" {
		t.Errorf("api_url: got %q, want flag value", got.APIURL)
	}
}

func TestResolveProfileSelectionPrecedence(t *testing.T) {
	cfg := &config.Config{
		DefaultProfile: "stewartbrothers",
		Profiles: map[string]config.Profile{
			"stewartbrothers": {Provider: "forgejo", APIURL: "https://sb/api/v1"},
			"github":          {Provider: "github", APIURL: "https://api.github.com"},
		},
	}

	t.Run("default_profile when nothing else", func(t *testing.T) {
		clearGaiaEnv(t)
		got, err := config.Resolve(cfg, config.Override{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile != "stewartbrothers" || got.Provider != "forgejo" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("env wins over default_profile", func(t *testing.T) {
		clearGaiaEnv(t)
		t.Setenv("GAIA_PROFILE", "github")
		got, err := config.Resolve(cfg, config.Override{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile != "github" || got.Provider != "github" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("flag wins over env and default", func(t *testing.T) {
		clearGaiaEnv(t)
		t.Setenv("GAIA_PROFILE", "github")
		got, err := config.Resolve(cfg, config.Override{Profile: "stewartbrothers"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile != "stewartbrothers" {
			t.Errorf("profile: got %q, want stewartbrothers (flag wins)", got.Profile)
		}
	})
}

func TestResolveProfileFlagWithNoConfigIsError(t *testing.T) {
	clearGaiaEnv(t)
	if _, err := config.Resolve(nil, config.Override{Profile: "x"}); err == nil {
		t.Fatal("--profile with nil config should error; got nil")
	}
	if _, err := config.Resolve(&config.Config{}, config.Override{Profile: "x"}); err == nil {
		t.Fatal("--profile with empty config should error; got nil")
	}
}

func TestResolveTokenEnvEmptyFallsBackToCanonical(t *testing.T) {
	// If profile.TokenEnv names an env var but it's empty, fall through
	// to the provider canonical (FORGEJO_TOKEN). This lets a user have a
	// profile that prefers a custom env var but still works in
	// environments where only the canonical is set.
	clearGaiaEnv(t)
	t.Setenv("GIT_FORGE_GITEA_TOKEN", "") // explicit empty
	t.Setenv("FORGEJO_TOKEN", "fjt-fallback")

	cfg := &config.Config{
		DefaultProfile: "sb",
		Profiles: map[string]config.Profile{
			"sb": {Provider: "forgejo", TokenEnv: "GIT_FORGE_GITEA_TOKEN"},
		},
	}
	got, _ := config.Resolve(cfg, config.Override{})
	if got.Token != "fjt-fallback" {
		t.Errorf("expected fallback to FORGEJO_TOKEN; got %q", got.Token)
	}
}

func TestResolveProfileNotFoundIsError(t *testing.T) {
	clearGaiaEnv(t)
	cfg := &config.Config{
		Profiles: map[string]config.Profile{"a": {Provider: "forgejo"}},
	}
	if _, err := config.Resolve(cfg, config.Override{Profile: "b"}); err == nil {
		t.Fatal("expected error for unknown profile; got nil")
	}
}

func TestResolveTokenFromTokenEnv(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("GIT_FORGE_GITEA_TOKEN", "the-real-token")
	t.Setenv("FORGEJO_TOKEN", "should-be-ignored")

	cfg := &config.Config{
		DefaultProfile: "sb",
		Profiles: map[string]config.Profile{
			"sb": {Provider: "forgejo", APIURL: "x", TokenEnv: "GIT_FORGE_GITEA_TOKEN"},
		},
	}
	got, _ := config.Resolve(cfg, config.Override{})
	if got.Token != "the-real-token" {
		t.Errorf("token_env should be honored; got %q", got.Token)
	}
}

func TestResolveTokenFromCanonicalForgejo(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "fjt")
	got, _ := config.Resolve(nil, config.Override{Provider: "forgejo"})
	if got.Token != "fjt" {
		t.Errorf("FORGEJO_TOKEN: got %q", got.Token)
	}
}

func TestResolveTokenFromCanonicalGitHub(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("GITHUB_TOKEN", "ght")
	got, _ := config.Resolve(nil, config.Override{Provider: "github"})
	if got.Token != "ght" {
		t.Errorf("GITHUB_TOKEN: got %q", got.Token)
	}
}

func TestResolveTokenAbsentIsEmptyNotError(t *testing.T) {
	clearGaiaEnv(t)
	got, err := config.Resolve(nil, config.Override{Provider: "forgejo"})
	if err != nil {
		t.Fatalf("Resolve with no token should not error: %v", err)
	}
	if got.Token != "" {
		t.Errorf("token: got %q, want empty", got.Token)
	}
}

func TestResolvedStringRedactsToken(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "very-secret-do-not-print")
	got, _ := config.Resolve(nil, config.Override{Provider: "forgejo"})

	s := got.String()
	if strings.Contains(s, "very-secret-do-not-print") {
		t.Fatalf("Resolved.String() must not include the token; got %q", s)
	}
	if !strings.Contains(s, "TokenSet:true") {
		t.Errorf("expected TokenSet:true in %q", s)
	}
}

func TestResolvedStringMissingTokenShows(t *testing.T) {
	clearGaiaEnv(t)
	got, _ := config.Resolve(nil, config.Override{Provider: "forgejo"})
	s := got.String()
	if !strings.Contains(s, "TokenSet:false") {
		t.Errorf("expected TokenSet:false in %q", s)
	}
}
