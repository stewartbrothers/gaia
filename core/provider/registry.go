package provider

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/stewartbrothers/gaia/core/cache"
)

// This file is the forge registry: the seam that lets a new forge become
// a purely additive `core/<forge>` package instead of an edit to a
// string switch in internal/forgebuilder (#309, ADR 0001 criterion 1 —
// GitLab/Bitbucket are named on the roadmap). Each forge package
// registers itself in init(); forgebuilder dispatches through [Build]
// and never names a forge.

// BuildConfig is the resolved, forge-agnostic input a [Factory] needs to
// construct a Provider. forgebuilder fills it from the settings handle
// (API URL + token) and the opened cache, so the forge packages don't
// depend on core/settings.
type BuildConfig struct {
	// APIURL is the resolved API base. Empty means "the forge's
	// default" — a forge with a well-known public endpoint (GitHub)
	// substitutes its own; a forge with no default (Forgejo) returns a
	// usage error from its Factory.
	APIURL string
	// Token is the resolved bearer. Empty is allowed (anonymous reads).
	Token string
	// Cache backs the provider's read cache. Nil disables caching.
	Cache cache.Cache
}

// Factory constructs a Provider from a [BuildConfig]. It must:
//   - return a usage error when a required field is missing for that
//     forge (e.g. Forgejo with no APIURL), rather than a half-built
//     Provider;
//   - be safe to call repeatedly (it holds no global state of its own);
//   - never block on I/O — construction is cheap; the first network
//     call happens later, on a Provider method.
type Factory func(BuildConfig) (Provider, error)

// Registration is everything the registry knows about one forge. A forge
// package builds one in its init() and passes it to [Register].
type Registration struct {
	// Name is the provider key matched against settings.Provider()
	// ("forgejo", "github", ...). Required, must be unique.
	Name string
	// Factory constructs the Provider. Required.
	Factory Factory
	// DefaultAPIURL is the forge's well-known public API endpoint, used
	// to fill the display host when no API URL is configured (GitHub:
	// "https://api.github.com"). Empty for self-hosted-only forges
	// (Forgejo) that have no universal default.
	DefaultAPIURL string
	// TokenEnvNames are the env var names checked, in order, for a
	// bearer when no profile/flag supplies one — gaia's canonical name
	// first, then the upstream-CLI convention (e.g. FORGEJO_TOKEN then
	// GITEA_TOKEN). The registry records them so a future change can
	// make core/config's token fallback registry-driven (#340); the
	// registry is already the source of truth for "what forges exist."
	TokenEnvNames []string
	// Unsupported lists the resource categories this provider does not
	// offer as a product (a hypothetical issues-only forge might list
	// CapPullRequests, CapWikis, CapReleases). Empty — the case for
	// Forgejo and GitHub — means "supports everything"; consumers then
	// hide nothing. See capabilities.go and [Supports]. This is static
	// compile-time knowledge, declared here rather than probed at
	// runtime (#342).
	Unsupported []Capability
}

// ErrUnknownProvider is returned (wrapped) by [Build] when no forge is
// registered under the requested name. Callers branch on it with
// errors.Is.
var ErrUnknownProvider = errors.New("provider: unknown provider")

var (
	registryMu sync.RWMutex
	registry   = map[string]Registration{}
)

// Register adds a forge to the registry. Called from forge package
// init() functions. It panics on a programming error — empty name, nil
// factory, or a duplicate name — because those are wiring bugs that must
// surface at startup, not silently shadow a forge at runtime. (Same
// contract as database/sql.Register.)
func Register(r Registration) {
	if r.Name == "" {
		panic("provider.Register: empty name")
	}
	if r.Factory == nil {
		panic("provider.Register: nil Factory for " + r.Name)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[r.Name]; dup {
		panic("provider.Register: duplicate provider " + r.Name)
	}
	registry[r.Name] = r
}

// Lookup returns the registration for name and whether it exists.
func Lookup(name string) (Registration, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	r, ok := registry[name]
	return r, ok
}

// Build constructs the Provider registered under name from cfg. An
// unregistered name returns an error wrapping [ErrUnknownProvider]; a
// Factory error (e.g. missing API URL) propagates verbatim.
func Build(name string, cfg BuildConfig) (Provider, error) {
	reg, ok := Lookup(name)
	if !ok {
		return nil, fmt.Errorf("%w %q (registered: %v)", ErrUnknownProvider, name, Registered())
	}
	return reg.Factory(cfg)
}

// Registered returns the names of every registered forge, sorted. Used
// for "supported: ..." error messages and diagnostics.
func Registered() []string {
	registryMu.RLock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	registryMu.RUnlock()
	sort.Strings(names)
	return names
}

// TokenEnvNames returns the ordered token env-var fallbacks declared by
// the named forge, or nil when the forge is unregistered or declared
// none. Exposed for the (#340) folding of core/config's envNamesFor.
func TokenEnvNames(name string) []string {
	if reg, ok := Lookup(name); ok {
		return reg.TokenEnvNames
	}
	return nil
}
