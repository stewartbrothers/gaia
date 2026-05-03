package cache

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDir returns the root directory under which per-(provider,host)
// SQLite files live: $XDG_CACHE_HOME/gaia, falling back to
// $HOME/.cache/gaia. The directory is NOT created here; Open does that
// with the right (0700) mode when needed.
func DefaultDir() (string, error) {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "gaia"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cache: locate home: %w", err)
	}
	return filepath.Join(home, ".cache", "gaia"), nil
}

// PathFor resolves the cache file path for a given (provider, apiURL)
// pair. The host is extracted from apiURL — non-URL strings are treated
// as raw hosts. The layout is:
//
//	<DefaultDir()>/<provider>/<host>.db
//
// One file per (provider, host) gives:
//   - cache poisoning isolation (a compromised forge cannot pollute
//     another forge's cache)
//   - easy nuke (rm <provider>/<host>.db)
//   - multi-process safety via SQLite's own file locking.
func PathFor(provider, apiURL string) (string, error) {
	if provider == "" {
		return "", errors.New("cache: provider is required")
	}
	host := hostFromAPIURL(apiURL)
	if host == "" {
		return "", fmt.Errorf("cache: cannot derive host from api_url %q", apiURL)
	}
	root, err := DefaultDir()
	if err != nil {
		return "", err
	}
	// Sanitize host: replace path separators just in case (a malformed
	// URL with `..` parts would otherwise escape the cache dir).
	host = strings.ReplaceAll(host, string(filepath.Separator), "_")
	host = strings.ReplaceAll(host, "/", "_")
	host = strings.ReplaceAll(host, "..", "_")
	return filepath.Join(root, provider, host+".db"), nil
}

// hostFromAPIURL extracts the host portion of an API URL. Falls back to
// returning the input verbatim if it doesn't parse as a URL — useful
// for tests that feed bare hostnames.
func hostFromAPIURL(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		// Bare hostname — accept it.
		return strings.ToLower(strings.TrimSpace(s))
	}
	return strings.ToLower(u.Host)
}
