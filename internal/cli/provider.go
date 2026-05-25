package cli

import (
	"sync"

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
		})
	})
	return flags.settings.s, flags.settings.err
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
