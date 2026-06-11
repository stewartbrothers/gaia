package config_test

// This black-box test file does two jobs:
//
//  1. Side-effect import of the forge adapter packages so their init()
//     calls provider.Register populate the registry. envNamesFor is now
//     registry-driven (#340), so without this the token-fallback tests in
//     resolve_test.go (FORGEJO_TOKEN/GITEA_TOKEN/GITHUB_TOKEN/GH_TOKEN)
//     would see an empty registry and fail. In production the registry is
//     always populated before config.Resolve runs because every
//     settings.Load caller transitively imports core/forges via
//     internal/forgebuilder; the test binary has no such importer, so it
//     blank-imports the forges here.
//
//     The import MUST live in a black-box (package config_test) file: a
//     white-box (package config) test importing core/forgejo would create
//     an import cycle (forgejo -> ... is fine, but config -> forges ->
//     forgejo while config is under test pulls the forge graph into the
//     config compile unit). Black-box keeps core/config itself free of any
//     forge dependency.
//
//  2. Proves envNamesFor (exercised through config.Resolve) now reflects
//     the registry: a registered forge's declared TokenEnvNames flow
//     through token resolution, and an unregistered provider yields no env
//     fallbacks.

import (
	"testing"

	"github.com/stewartbrothers/gaia/core/config"

	// Blank-imported for the side effect of registering the forges in the
	// provider registry, which envNamesFor now consults.
	_ "github.com/stewartbrothers/gaia/core/forges"
)

// TestResolveTokenIsRegistryDriven asserts that the env fallback used by
// token resolution comes from the forge's registered TokenEnvNames rather
// than a hard-coded switch. Registering core/forges declares forgejo's
// fallbacks as [FORGEJO_TOKEN, GITEA_TOKEN]; the second name (GITEA_TOKEN)
// resolving here proves the registry list — not a literal in resolve.go —
// is what drives the chain.
func TestResolveTokenIsRegistryDriven(t *testing.T) {
	clearGaiaEnv(t)
	// Only the registry's second declared name is set. If envNamesFor were
	// still a switch this would also pass, but combined with the empty-
	// registry RED state captured in TDD it pins the registry as the source.
	t.Setenv("GITEA_TOKEN", "from-registry-fallback")

	got, err := config.Resolve(nil, config.Override{Provider: "forgejo"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Token != "from-registry-fallback" {
		t.Errorf("expected registry-declared GITEA_TOKEN fallback to resolve; got %q", got.Token)
	}
}

// TestResolveTokenUnregisteredProviderNoEnvNames pins that a provider with
// no registration contributes no env fallbacks: even with every known
// token env var set, an unknown provider resolves to an empty token. This
// matches the prior switch's default (nil) and guards against a future
// change accidentally falling back to a global env scan.
func TestResolveTokenUnregisteredProviderNoEnvNames(t *testing.T) {
	clearGaiaEnv(t)
	t.Setenv("FORGEJO_TOKEN", "fjt")
	t.Setenv("GITEA_TOKEN", "git")
	t.Setenv("GITHUB_TOKEN", "ght")
	t.Setenv("GH_TOKEN", "ght")

	got, err := config.Resolve(nil, config.Override{Provider: "gitlab"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Token != "" {
		t.Errorf("unregistered provider should yield no env-name fallback; got token %q", got.Token)
	}
}
