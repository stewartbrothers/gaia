package settings

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/config"
)

// The git remote is a clue to where the forge is (#412). gaia never
// manages remotes; it reads origin's URL and uses the host and
// owner/name. These helpers turn that clue into (a) a forge when
// nothing is configured and (b) a yes/no on whether the remote can
// name a repo on the forge that config chose.

// gitHost returns the hostname (no port) that a forge serves git
// from, given its API URL. GitHub's API lives on api.github.com but
// git on github.com; Forgejo and Aikestra share the API's hostname
// (Forgejo setups may also push to a second host, see
// remoteFitsProvider).
func gitHost(provider, apiURL string) string {
	h := hostname(apiURL)
	if provider == "github" && (h == "" || h == "api.github.com") {
		return "github.com"
	}
	return h
}

func hostname(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// remoteFitsProvider reports whether a git remote on host can name a
// repo on the active forge. others lists the git hosts of the other
// configured forges (profiles and stored logins). A false answer
// carries the reason `config doctor` prints.
//
// Forgejo is matched loosely on purpose: checkouts commonly push over
// SSH to one host and call the API on another (repo.* vs git.*), so
// a Forgejo profile accepts any remote that isn't plainly another
// forge's.
func remoteFitsProvider(provider, apiURL, host string, others []string) (bool, string) {
	own := gitHost(provider, apiURL)
	switch provider {
	case "aikestra":
		if own != "" && own == host {
			return true, ""
		}
		return false, fmt.Sprintf("remote host %s is not this Aikestra instance's host", host)
	case "github":
		if host == own {
			return true, ""
		}
		return false, fmt.Sprintf("remote host %s is not a GitHub host", host)
	case "forgejo":
		if host == "github.com" {
			return false, "remote host github.com is not a Forgejo host"
		}
		if host != own && slices.Contains(others, host) {
			return false, fmt.Sprintf("remote host %s belongs to another configured forge", host)
		}
	}
	return true, ""
}

// otherForgeHosts lists the git hosts of every configured forge
// other than the active one: profiles in the merged config plus
// stored logins.
func otherForgeHosts(provider, apiURL string, cfg *config.Config, logins []storedLogin) []string {
	own := gitHost(provider, apiURL)
	var out []string
	add := func(h string) {
		if h != "" && h != own && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	if cfg != nil {
		for _, p := range cfg.Profiles {
			add(gitHost(p.Provider, p.APIURL))
		}
	}
	for _, sl := range logins {
		add(loginGitHost(sl))
	}
	return out
}

// storedLogin is one entry from the layered credential store.
type storedLogin struct {
	provider, host string
	cred           auth.Credential
}

// storedLogins lists every credential across both layers, project
// first; a global entry for the same provider:host is shadowed.
func storedLogins(l *auth.Layered) []storedLogin {
	seen := map[string]bool{}
	var out []storedLogin
	for _, st := range []*auth.Store{l.Project, l.Global} {
		if st == nil {
			continue
		}
		for _, key := range st.Hosts() {
			p, h, ok := splitProviderHost(key)
			if !ok || seen[key] {
				continue
			}
			seen[key] = true
			c, _ := st.Get(p, h)
			out = append(out, storedLogin{p, h, c})
		}
	}
	return out
}

// loginGitHost is a stored login's git hostname. The API URL is
// preferred; the store key's host stands in when it's absent.
func loginGitHost(sl storedLogin) string {
	api := sl.cred.APIURL
	if api == "" && sl.host != "" {
		api = "https://" + sl.host
	}
	return gitHost(sl.provider, api)
}

// forgeForRemote picks the forge a remote on host belongs to, for use
// only when config names no forge. A stored login on the same
// hostname wins; github.com means GitHub even with no login (the
// token can come from env). Two logins on different forges for the
// same hostname are ambiguous, so the caller falls back.
func forgeForRemote(host string, logins []storedLogin) (provider string, cred auth.Credential, ok bool) {
	var match *storedLogin
	for i := range logins {
		if loginGitHost(logins[i]) != host {
			continue
		}
		if match != nil && match.provider != logins[i].provider {
			return "", auth.Credential{}, false
		}
		if match == nil {
			match = &logins[i]
		}
	}
	if match != nil {
		return match.provider, match.cred, true
	}
	if host == "github.com" {
		return "github", auth.Credential{}, true
	}
	return "", auth.Credential{}, false
}
