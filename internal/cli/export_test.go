package cli

// NormalizeForgejoURLForTest exposes the internal normalizeForgejoURL
// helper to the cli_test package. Test-only re-export.
func NormalizeForgejoURLForTest(raw string) string {
	return normalizeForgejoURL(raw)
}

// SetGithubAPIURLForTest swaps the package-level githubAPIURL so an
// httptest server can stand in for api.github.com. Returns the prior
// value so a deferred call can restore it.
func SetGithubAPIURLForTest(u string) string {
	old := githubAPIURL
	githubAPIURL = u
	return old
}
