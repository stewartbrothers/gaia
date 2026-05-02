// Package autodetect derives provider+owner+repo context from the
// current directory's git remote, so most invocations don't need
// `--repo` or `--provider`. Callers (the CLI in #15) overlay the
// detected values into config.Override before calling config.Resolve;
// explicit flags always win.
package autodetect

import (
	"fmt"
	"net/url"
	"strings"
)

// Repo describes a parsed git remote URL: which forge host, which
// owner, which repo name. Provider is left empty here — call
// ProviderFor(repo.Host, ...) to get the provider name.
type Repo struct {
	Host  string
	Owner string
	Name  string
}

// ParseRemoteURL parses an SSH or HTTPS git remote URL into a Repo.
// Accepts the SCP-like form (`git@host:owner/name(.git)?`), the
// `ssh://` form, the `https://` form, and the `git://` form. Sub-group
// paths (gitlab style `group/sub/repo`) are NOT supported and return
// an error — Forgejo and GitHub both use `owner/name`.
func ParseRemoteURL(raw string) (*Repo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("autodetect: empty remote URL")
	}

	host, path, err := splitHostPath(raw)
	if err != nil {
		return nil, err
	}

	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("autodetect: expected owner/name path, got %q (from %q)", path, raw)
	}
	if host == "" {
		return nil, fmt.Errorf("autodetect: empty host in %q", raw)
	}
	return &Repo{Host: host, Owner: parts[0], Name: parts[1]}, nil
}

// ProviderFor maps a git host to a gaia provider name. github.com is
// the one built-in mapping; everything else defaults to "forgejo"
// since gaia targets self-hosted Forgejo as the primary use case. The
// hostsAllowlist parameter is reserved for future tightening (e.g.
// strict mode where unknown hosts are rejected); it is currently
// non-load-bearing.
func ProviderFor(host string, hostsAllowlist []string) string {
	_ = hostsAllowlist
	if host == "github.com" {
		return "github"
	}
	return "forgejo"
}

// splitHostPath returns (host, owner-and-name-path) from any of the
// supported remote URL forms.
func splitHostPath(raw string) (host, path string, err error) {
	if strings.Contains(raw, "://") {
		u, perr := url.Parse(raw)
		if perr != nil {
			return "", "", fmt.Errorf("autodetect: parse %q: %w", raw, perr)
		}
		return u.Hostname(), strings.TrimPrefix(u.Path, "/"), nil
	}

	// SCP-like: [user@]host:path
	colon := strings.Index(raw, ":")
	if colon < 0 {
		return "", "", fmt.Errorf("autodetect: not a recognized remote URL: %q", raw)
	}
	hostPart := raw[:colon]
	if at := strings.Index(hostPart, "@"); at >= 0 {
		hostPart = hostPart[at+1:]
	}
	return hostPart, raw[colon+1:], nil
}
