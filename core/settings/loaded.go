package settings

import (
	"net/url"
	"sort"
	"strings"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/config"
)

// loadedSettings is the in-memory snapshot produced by Load. All fields
// are populated eagerly; method receivers do no I/O.
type loadedSettings struct {
	// Resolved scalars
	profile     string
	provider    string
	apiURL      string
	token       string
	defaultRepo string
	repo        ownerName

	// Layered raw views (for Inspector)
	globalConfig           *config.Config
	projectConfig          *config.Config
	globalConfigPath       string
	projectConfigPath      string
	credentials            *auth.Layered
	globalCredentialsPath  string
	projectCredentialsPath string

	// Context
	cwd           string
	repoRoot      string
	gitRemoteRepo string
	envVars       map[string]bool

	// Unresolved overrides retained for Inspector
	profileFlag string
	repoFlag    string

	// Cache settings
	cache CacheSettings
}

type ownerName struct {
	owner, name string
	ok          bool
}

// --- Settings ---------------------------------------------------

func (s *loadedSettings) Profile() string             { return s.profile }
func (s *loadedSettings) Provider() string            { return s.provider }
func (s *loadedSettings) APIURL() string              { return s.apiURL }
func (s *loadedSettings) Token() string               { return s.token }
func (s *loadedSettings) DefaultRepo() string         { return s.defaultRepo }
func (s *loadedSettings) Cache() CacheSettings        { return s.cache }
func (s *loadedSettings) Inspector() Inspector        { return (*loadedInspector)(s) }
func (s *loadedSettings) Repo() (string, string, bool) {
	return s.repo.owner, s.repo.name, s.repo.ok
}

// --- Inspector --------------------------------------------------
//
// loadedInspector is a type alias over loadedSettings so the
// inspector methods don't pollute the Settings method set. Same
// underlying memory; different method set surfaced to callers.
type loadedInspector loadedSettings

func (i *loadedInspector) GlobalConfig() *config.Config       { return i.globalConfig }
func (i *loadedInspector) ProjectConfig() *config.Config      { return i.projectConfig }
func (i *loadedInspector) GlobalConfigPath() string           { return i.globalConfigPath }
func (i *loadedInspector) ProjectConfigPath() string          { return i.projectConfigPath }
func (i *loadedInspector) Credentials() *auth.Layered         { return i.credentials }
func (i *loadedInspector) GlobalCredentialsPath() string      { return i.globalCredentialsPath }
func (i *loadedInspector) ProjectCredentialsPath() string     { return i.projectCredentialsPath }
func (i *loadedInspector) Cwd() string                        { return i.cwd }
func (i *loadedInspector) RepoRoot() string                   { return i.repoRoot }
func (i *loadedInspector) GitRemoteRepo() string              { return i.gitRemoteRepo }
func (i *loadedInspector) EnvVars() map[string]bool           { return i.envVars }
func (i *loadedInspector) ProfileFlag() string                { return i.profileFlag }
func (i *loadedInspector) RepoFlag() string                   { return i.repoFlag }

// --- helpers ----------------------------------------------------

// hostFromAPIURL extracts the host component of a parsed API URL, or
// "" when the URL is empty or unparseable. Used to key credential
// lookups by host (the same key gaia auth stores under).
func hostFromAPIURL(apiURL string) string {
	if apiURL == "" {
		return ""
	}
	u, err := url.Parse(apiURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// parseOwnerName splits "owner/name" into its components. Returns
// ok=false when the input is malformed; callers that surface a usage
// error rely on the standard "no --repo or autodetect" message in
// resolveRepo rather than a per-malformed-string complaint here.
func parseOwnerName(slug string) ownerName {
	i := strings.IndexByte(slug, '/')
	if i <= 0 || i == len(slug)-1 {
		return ownerName{}
	}
	return ownerName{owner: slug[:i], name: slug[i+1:], ok: true}
}

// singleCredential returns the lone (provider, host, cred) when the
// layered credentials store has exactly one entry across both layers.
// Used for the "ran `gaia auth gh` once, no config yet" sole-credential
// fallback. Returns ok=false for zero or many entries.
func singleCredential(l *auth.Layered) (string, string, auth.Credential, bool) {
	type item struct {
		provider, host string
		cred           auth.Credential
	}
	seen := map[string]struct{}{}
	var items []item
	collect := func(st *auth.Store, source string) {
		if st == nil {
			return
		}
		for _, key := range st.Hosts() {
			p, h, ok := splitProviderHost(key)
			if !ok {
				continue
			}
			pkey := p + ":" + h
			if _, dup := seen[pkey]; dup && source == "global" {
				continue
			}
			seen[pkey] = struct{}{}
			c, _ := st.Get(p, h)
			items = append(items, item{p, h, c})
		}
	}
	collect(l.Project, "project")
	collect(l.Global, "global")
	sort.Slice(items, func(i, j int) bool {
		return items[i].provider+":"+items[i].host < items[j].provider+":"+items[j].host
	})
	if len(items) != 1 {
		return "", "", auth.Credential{}, false
	}
	return items[0].provider, items[0].host, items[0].cred, true
}

// splitProviderHost mirrors forgebuilder.splitProviderHost. Kept
// in-package because Settings owns the resolution flow now.
func splitProviderHost(key string) (provider, host string, ok bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}
