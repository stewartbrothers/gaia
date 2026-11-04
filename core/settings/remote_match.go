package settings

import "fmt"

// remoteFitsProvider reports whether a git remote on host can name a
// repo on the active provider. Autodetect uses the remote only when it
// does; otherwise default_repo applies (#412). A false answer carries
// the reason `config doctor` prints.
//
// The rule is by provider kind, not by matching the remote host to
// the API host: Forgejo checkouts commonly push over SSH to one host
// and serve the API from another (repo.* vs git.*), and a strict host
// match would break them.
func remoteFitsProvider(provider, apiURL, host string) (bool, string) {
	switch provider {
	case "aikestra":
		return false, "the aikestra provider does not host git repos"
	case "github":
		if host == "github.com" {
			return true, ""
		}
		if api := hostFromAPIURL(apiURL); api != "" && api != "api.github.com" && api == host {
			return true, "" // GitHub Enterprise: API and git share a host.
		}
		return false, fmt.Sprintf("remote host %s is not a GitHub host", host)
	case "forgejo":
		if host == "github.com" {
			return false, "remote host github.com is not a Forgejo host"
		}
	}
	return true, ""
}
