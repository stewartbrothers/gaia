package settings

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/autodetect"
	"github.com/stewartbrothers/gaia/core/config"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// Load resolves the full configuration + credentials + env + git
// remote snapshot in one eager pass and returns a Settings handle.
//
// Errors are returned only for I/O failures the caller can't paper
// over (locating $HOME, parsing corrupt YAML) and for misconfiguration
// the operator must fix (Override.Profile naming a profile the merged
// config doesn't define). A missing config file is the normal
// "env-only" case — it is not an error.
//
// Call once at the root command's PersistentPreRunE; thread the
// result through context via WithSettings.
func Load(ov Override) (Settings, error) {
	s := &loadedSettings{
		profileFlag: ov.Profile,
		repoFlag:    ov.Repo,
	}

	// --- Config layers --------------------------------------------
	gPath, err := config.DefaultPath()
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "locate global config")
	}
	s.globalConfigPath = gPath
	globalCfg, err := config.Load(gPath)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "load global config")
	}
	s.globalConfig = globalCfg

	cwd := ov.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	s.cwd = cwd

	if root := auth.ProjectRoot(cwd); root != "" {
		s.repoRoot = root
		pPath := config.ProjectPath(root)
		s.projectConfigPath = pPath
		projectCfg, perr := config.Load(pPath)
		if perr != nil {
			return nil, exitcode.Wrap(perr, exitcode.Generic, "load project config")
		}
		s.projectConfig = projectCfg
	}

	mergedCfg := config.Merge(s.globalConfig, s.projectConfig)

	// --- Resolve (provider, api url, token) -----------------------
	//
	// Resolve is *strict* on missing profile names — fine for the
	// production path where the operator typed `--profile foo`, but
	// fatal for `gaia config doctor`, whose whole job is to inspect
	// broken configs. The original cli/config.go's buildDoctorInputs
	// deliberately skipped Resolve for this reason.
	//
	// Compromise: surface Resolve errors as fatal ONLY when the
	// operator explicitly named a profile via --profile. Implicit
	// resolution failures (default_profile pointing at a missing
	// entry, no default_profile when profiles exist) degrade to
	// empty Resolved — production callers see s.Provider()=="" and
	// surface their own usage error; doctor inspects the raw
	// layers via Inspector().
	resolved, err := config.Resolve(mergedCfg, config.Override{
		Profile:  ov.Profile,
		Provider: ov.Provider,
		APIURL:   ov.APIURL,
	})
	if err != nil {
		if ov.Profile != "" {
			return nil, exitcode.Wrap(err, exitcode.Usage, "resolve config")
		}
		resolved = &config.Resolved{}
	}
	s.profile = resolved.Profile
	s.provider = resolved.Provider
	s.apiURL = resolved.APIURL
	s.token = resolved.Token
	s.defaultRepo = resolved.DefaultRepo

	// Per-request token override (gaia-mcp HTTP transport plumbs a
	// per-call bearer through here so each request acts as the
	// caller's identity).
	if ov.Token != "" {
		s.token = ov.Token
	}

	// --- Credentials ----------------------------------------------
	credsPath, err := auth.DefaultGlobalPath()
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "locate global credentials")
	}
	s.globalCredentialsPath = credsPath
	globalStore, err := auth.Load(credsPath)
	if err != nil {
		return nil, exitcode.Wrap(err, exitcode.Generic, "load global credentials")
	}
	var projectStore *auth.Store
	if s.repoRoot != "" {
		projectCredsPath := auth.ProjectPath(s.repoRoot)
		s.projectCredentialsPath = projectCredsPath
		projectStore, err = auth.Load(projectCredsPath)
		if err != nil {
			return nil, exitcode.Wrap(err, exitcode.Generic, "load project credentials")
		}
	}
	s.credentials = &auth.Layered{Global: globalStore, Project: projectStore}

	// --- Git remote: a clue to the forge and the repo (#412) -----
	var remote *autodetect.Repo
	if ov.Repo == "" {
		if d, derr := autodetect.FromGitRemoteCached(context.Background(), ov.Cache, cwd, ""); derr == nil {
			remote = d
		}
	}
	logins := storedLogins(s.credentials)

	// If neither config nor env named a forge, lean on the remote: a
	// stored login on the remote's hostname, or GitHub for github.com.
	// Failing that, the sole-credential heuristic (covers the "ran
	// `gaia auth gh` once, no config yet" path).
	if s.provider == "" && s.apiURL == "" {
		if remote != nil {
			if p, cred, ok := forgeForRemote(remote.Host, logins); ok {
				s.provider = p
				s.apiURL = cred.APIURL
				if s.token == "" {
					s.token = cred.Token
				}
				if s.token == "" {
					s.token = config.TokenFromEnv(p)
				}
				s.forgeFromRemote = fmt.Sprintf("forge %s picked from git remote host %s", p, remote.Host)
			}
		}
		if s.provider == "" {
			if soleProvider, _, soleCred, ok := singleCredential(s.credentials); ok {
				s.provider = soleProvider
				s.apiURL = soleCred.APIURL
				if s.token == "" {
					s.token = soleCred.Token
				}
			}
		}
	}

	// Stored-credential token fallback when env and per-request layers
	// didn't supply one.
	if s.token == "" && s.provider != "" {
		host := hostFromAPIURL(s.apiURL)
		if c, _, ok := s.credentials.Get(s.provider, host); ok {
			s.token = c.Token
		}
	}

	// --- Env var snapshot (presence only) -------------------------
	s.envVars = map[string]bool{}
	for _, n := range tokenEnvNames {
		s.envVars[n] = os.Getenv(n) != ""
	}
	// Profile-pinned TokenEnv: doctor wants to know whether the
	// operator's chosen env name is set, not just the canonical
	// fallbacks. Capture it here so the eager snapshot is complete.
	if s.profile != "" {
		if p, ok := mergedCfg.Profiles[s.profile]; ok && p.TokenEnv != "" {
			s.envVars[p.TokenEnv] = os.Getenv(p.TokenEnv) != ""
		}
	}

	// --- Repo resolution: flag > git remote (when it fits the forge,
	// #412) > project default_repo -----------------------------------
	switch {
	case ov.Repo != "":
		s.repo = parseOwnerName(ov.Repo)
	default:
		if remote != nil {
			slug := remote.Owner + "/" + remote.Name
			fits, why := remoteFitsProvider(s.provider, s.apiURL, remote.Host, otherForgeHosts(s.provider, s.apiURL, mergedCfg, logins))
			if fits {
				s.repo = ownerName{owner: remote.Owner, name: remote.Name, ok: true}
				s.gitRemoteRepo = slug
			} else {
				s.gitRemoteSkipped = fmt.Sprintf("git remote %s skipped: %s", slug, why)
			}
		}
		if !s.repo.ok && s.defaultRepo != "" {
			s.repo = parseOwnerName(s.defaultRepo)
		}
	}

	// --- Cache settings -------------------------------------------
	s.cache = CacheSettings{
		Enabled:   config.CacheEnabled(mergedCfg.Cache),
		SingleTTL: time.Duration(config.CacheTTLSingleSeconds(mergedCfg.Cache)) * time.Second,
		ListTTL:   time.Duration(config.CacheTTLListSeconds(mergedCfg.Cache)) * time.Second,
		NoCache:   ov.NoCache || cacheDisabledByEnv(),
	}
	if mergedCfg.Cache != nil {
		s.cache.MaxSizeMB = mergedCfg.Cache.MaxSizeMB
	}

	return s, nil
}

// tokenEnvNames is the set of names whose presence the snapshot
// captures. Mirrors the production token-fallback chain plus the
// vendor-conventional aliases doctor reports on.
var tokenEnvNames = []string{
	"FORGEJO_TOKEN", "GITEA_TOKEN",
	"GITHUB_TOKEN", "GH_TOKEN",
	"AIKESTRA_TOKEN",
}

// cacheDisabledByEnv mirrors forgebuilder's helper of the same name —
// duplicated here on purpose: settings.Load is the new authority on
// "is the cache off?" so it owns the env-var check itself rather than
// reaching into forgebuilder for it. When forgebuilder migrates onto
// Settings, the original helper goes away. (#303)
func cacheDisabledByEnv() bool {
	switch strings.ToLower(os.Getenv("GAIA_CACHE_ENABLED")) {
	case "false", "0", "no":
		return true
	}
	return false
}
