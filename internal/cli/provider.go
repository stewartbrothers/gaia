package cli

import (
	"os"
	"strings"
	"sync"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/cache/sqlite"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/settings"
	"github.com/stewartbrothers/gaia/internal/forgebuilder"
)

// providerInfo carries the metadata a subcommand needs to render
// alongside the Provider's own results (host name in `whoami`, etc.).
// Local alias of forgebuilder.Info so subcommands keep their existing
// type reference; the underlying value is identical.
type providerInfo = forgebuilder.Info

// settingsCache is the per-invocation cache of the resolved
// [settings.Settings]. Populated by loadSettings on first call;
// subsequent calls within the same NewRootCmd invocation return the
// cached value. Created fresh in NewRootCmd so test harnesses that
// build many root commands in one process don't bleed Settings
// across invocations.
type settingsCache struct {
	once sync.Once
	s    settings.Settings
	err  error
}

// loadSettings resolves the Settings handle for this invocation. The
// first call performs settings.Load against flag-supplied overrides;
// every subsequent call within the same root command returns the
// cached value. This is the load-once-per-process guarantee called
// out by issue #311 — enforced by construction rather than by a
// process-global sync.Once (which would bleed across tests).
func loadSettings(flags *globalFlags) (settings.Settings, error) {
	if flags.settings == nil {
		// Defensive: every NewRootCmd initialises this. A nil here
		// would be a test that constructs a globalFlags by hand
		// without going through NewRootCmd — fall back to a one-off
		// load so the test still works.
		flags.settings = &settingsCache{}
	}
	flags.settings.once.Do(func() {
		flags.settings.s, flags.settings.err = settings.Load(settings.Override{
			Profile:  flags.Profile,
			Provider: flags.Provider,
			APIURL:   flags.APIURL,
			Repo:     flags.Repo,
			NoCache:  flags.NoCache,
			Cache:    autodetectCache(flags),
		})
	})
	return flags.settings.s, flags.settings.err
}

// autodetectCache opens the small "meta" SQLite cache that backs the
// git-remote autodetect lookup (#314), or returns nil when caching is
// disabled or the file can't be opened. It is deliberately separate from
// the per-(provider, host) forge cache: autodetect runs before the
// provider is resolved, so its result can't live in a provider-scoped
// DB. The file lands under the shared cache dir as meta/autodetect.db,
// so `gaia cache nuke` (which walks every .db under that root) clears it
// too.
//
// Gating mirrors what the test harnesses already toggle: --no-cache and
// GAIA_CACHE_ENABLED=false both skip the open, so golden/CLI tests pay
// no SQLite cost and never touch the real cache dir.
func autodetectCache(flags *globalFlags) cache.Cache {
	if flags.NoCache || strings.EqualFold(os.Getenv("GAIA_CACHE_ENABLED"), "false") {
		return nil
	}
	path, err := cache.PathFor("meta", "autodetect")
	if err != nil {
		return nil
	}
	c, err := sqlite.Open(path)
	if err != nil {
		return nil
	}
	return c
}

// buildForgejoProvider is the legacy name kept for backward-compat
// with subcommand call sites. Returns the provider.Provider interface
// so calls dispatch by the resolved provider — Forgejo or GitHub. The
// name stays "buildForgejoProvider" to avoid touching every CLI file;
// renaming is a follow-up cosmetic.
func buildForgejoProvider(flags *globalFlags) (provider.Provider, *providerInfo, error) {
	s, err := loadSettings(flags)
	if err != nil {
		return nil, nil, err
	}
	return forgebuilder.Build(s, forgebuilder.BuildOverride{})
}

// buildLabelOps / buildReleaseOps / buildWebhookOps build the provider
// and hand the handler only the narrow resource port it needs (#312).
// The returned value is the full provider under the hood, but typing it
// to the port means a label handler physically cannot reach, say,
// CreateIssue — the compiler enforces the slice. The providerInfo is
// dropped because these handlers don't render host metadata.
func buildLabelOps(flags *globalFlags) (provider.LabelOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildReleaseOps(flags *globalFlags) (provider.ReleaseOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildWebhookOps(flags *globalFlags) (provider.WebhookOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildSecretsOps(flags *globalFlags) (provider.SecretsOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildVariablesOps(flags *globalFlags) (provider.VariablesOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildRunnersOps(flags *globalFlags) (provider.RunnersOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildCollaboratorsOps(flags *globalFlags) (provider.CollaboratorsOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildBranchOps(flags *globalFlags) (provider.BranchOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}

func buildBranchProtectionOps(flags *globalFlags) (provider.BranchProtectionOps, error) {
	p, _, err := buildForgejoProvider(flags)
	return p, err
}
