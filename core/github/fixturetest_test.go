package github_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureServer returns an httptest.Server that replays one or more
// recorded api.github.com response bodies from testdata/fixtures.
//
// Two forms:
//
//  1. Single fixture: every request returns the named body.
//     fixtureServer(t, map[string]string{"": "cli-cli-issues-list.json"})
//
//  2. Path-prefix routing: longest-matching prefix wins, "" is the
//     fallback. Used when one method-under-test makes multiple HTTP
//     calls (e.g., GetPullRequest with WithCISummary fetches both
//     /pulls/{n} and /commits/{sha}/check-runs).
//
// Fixtures are raw JSON bodies captured from api.github.com via
// scripts/record-gh-fixtures.sh. The header set is fixed:
// `Content-Type: application/json`. Status defaults to 200 — fixtures
// are happy-path responses; error-path tests use the existing
// httptest.NewServer pattern.
func fixtureServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	loaded := map[string][]byte{}
	for prefix, name := range routes {
		body, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
		if err != nil {
			t.Fatalf("load fixture %s: %v", name, err)
		}
		loaded[prefix] = body
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := matchRoute(loaded, r.URL.Path)
		if body == nil {
			t.Errorf("no fixture for path %q (have prefixes %v)", r.URL.Path, prefixes(loaded))
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func matchRoute(routes map[string][]byte, path string) []byte {
	bestPrefix := ""
	for p := range routes {
		if p != "" && strings.HasPrefix(path, p) && len(p) > len(bestPrefix) {
			bestPrefix = p
		}
	}
	if body, ok := routes[bestPrefix]; ok && bestPrefix != "" {
		return body
	}
	return routes[""]
}

func prefixes(routes map[string][]byte) []string {
	out := make([]string, 0, len(routes))
	for p := range routes {
		if p == "" {
			out = append(out, "<fallback>")
		} else {
			out = append(out, p)
		}
	}
	return out
}
