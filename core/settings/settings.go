// Package settings is gaia's single read handle for resolved
// configuration, credentials, and environment. It exists to collapse
// the load → merge → resolve sequence — previously repeated at
// forgebuilder, cli/config (doctor input building), cli/repo, and
// inside doctor itself — into one in-memory snapshot that every
// subcommand consults.
//
// Sanctioned by docs/adr/0001-internal-interfaces.md and tracked at
// issue #311. The interface earns its place under ADR criterion (2):
// six unrelated consumers want narrow slices of the layered config +
// credentials state; threading the wide concrete types through each
// site couples consumers to concerns they do not have.
//
// # Lifecycle
//
// Load is called once at the root command's PersistentPreRunE. The
// resulting Settings is stashed on the cobra command's context and
// reached via FromContext. Subcommands never call Load themselves.
//
// Load is eager: every field is computed during the call and the
// returned Settings reads from cached state. There is no concurrency
// hazard on the read path because no I/O happens after Load returns.
//
// # Test substitution
//
// Settings is an interface so tests can supply a Fake without
// touching disk. Tests use the Fake helper (see fake.go) or implement
// the interface inline.
package settings

import (
	"context"
	"time"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/config"
)

// Settings is the read surface every subcommand depends on. High-level
// methods (Profile, Provider, APIURL, Token, Repo, Cache, DefaultRepo)
// cover every production path. Inspector returns the raw layered view
// `gaia config doctor` walks to render its findings; non-diagnostic
// code should NOT call Inspector.
type Settings interface {
	// Profile is the resolved profile name after flag > env > config
	// layering. Empty when no profile is configured.
	Profile() string

	// Provider is the resolved provider name (e.g., "forgejo",
	// "github"). Empty when nothing in the layers names one — callers
	// constructing a Provider should check for this and surface a
	// usage error.
	Provider() string

	// APIURL is the resolved API base URL for the active provider.
	APIURL() string

	// Token returns the resolved auth token. Never logged. Empty when
	// no credential was found at any layer.
	Token() string

	// Repo returns the resolved (owner, name) and ok=true if a target
	// repo could be derived from the --repo flag, git-remote
	// autodetect, or project default_repo, in that order. ok=false
	// means the caller's command needs a repo and should fail with
	// the standard usage error.
	Repo() (owner, name string, ok bool)

	// DefaultRepo returns the project-config-supplied owner/name
	// shortcut (.gaia/config.yaml's default_repo field). Empty when
	// absent. Most callers prefer Repo(); this is exposed for
	// diagnostics that want to distinguish the source.
	DefaultRepo() string

	// Cache returns the resolved cache settings — the result of
	// layering --no-cache, GAIA_CACHE_ENABLED, and the cache block
	// of config.yaml.
	Cache() CacheSettings

	// Inspector returns the diagnostic escape hatch — the raw layers
	// and paths needed by `gaia config doctor`. Production code
	// outside diagnostics should not use this.
	Inspector() Inspector
}

// Inspector is the diagnostic-only view of the underlying layers.
// Exposes the raw config and credential layers, the paths they came
// from, the cwd, the git-remote autodetect result, and a snapshot of
// which token env vars were set at Load time. Callers do NOT mutate
// the returned values.
type Inspector interface {
	GlobalConfig() *config.Config
	ProjectConfig() *config.Config
	GlobalConfigPath() string
	ProjectConfigPath() string
	Credentials() *auth.Layered
	GlobalCredentialsPath() string
	ProjectCredentialsPath() string
	Cwd() string
	RepoRoot() string
	GitRemoteRepo() string

	// EnvVars is a presence-only snapshot — names known to be set at
	// Load time map to true. Values are never exposed. Doctor uses
	// this to flag missing token env vars without ever reading a
	// token.
	EnvVars() map[string]bool

	// ProfileFlag returns the user's --profile flag value (unresolved).
	// Distinct from Settings.Profile(), which is the resolved name
	// after env + config defaults.
	ProfileFlag() string

	// RepoFlag returns the user's --repo flag value (unresolved).
	RepoFlag() string
}

// CacheSettings is the resolved cache configuration.
type CacheSettings struct {
	// Enabled is the merged config result before the per-invocation
	// NoCache override. Doctor reads this to report "the config says
	// caching is on" independent of any one command's flag.
	Enabled bool
	// SingleTTL is the resolved TTL for single-resource reads.
	SingleTTL time.Duration
	// ListTTL is the resolved TTL for list-style reads.
	ListTTL time.Duration
	// MaxSizeMB is the resolved size cap (0 = use the default).
	MaxSizeMB int
	// NoCache is the per-invocation bypass — the --no-cache flag or
	// GAIA_CACHE_ENABLED=false. When true, Provider construction
	// skips opening the cache regardless of Enabled.
	NoCache bool
}

// Override is the caller-supplied override layer Load applies on top
// of env + config. Empty fields mean "no override at this layer."
type Override struct {
	Profile  string
	Provider string
	APIURL   string
	Token    string
	Repo     string
	NoCache  bool
	// Cwd lets tests pin the working directory used for project-config
	// and git-remote autodetect. Empty (the production default) calls
	// os.Getwd at Load time.
	Cwd string
}

// ctxKey is the unexported type used to stash and retrieve Settings
// from a context. Unexported so no caller outside this package can
// race-stash a Settings under the same key.
type ctxKey struct{}

// WithSettings returns a derived context carrying s. The root command
// stashes once at PersistentPreRunE; subcommands read via FromContext.
func WithSettings(ctx context.Context, s Settings) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext returns the Settings stashed on ctx, or (nil, false) if
// none is present. Subcommands always expect ok=true; ok=false is a
// programming error (the root command did not call WithSettings).
func FromContext(ctx context.Context) (Settings, bool) {
	s, ok := ctx.Value(ctxKey{}).(Settings)
	return s, ok
}
