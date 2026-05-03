package cli

import (
	"io"

	"github.com/stewartbrothers/gaia/core/chain"
)

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

// SetGitRunnerForTest swaps the gitRunnerForTest hook used by
// `gaia pr checkout` so tests can intercept git subprocess
// invocations without spawning git. nil restores the production
// execGit.
func SetGitRunnerForTest(fn gitRunner) {
	gitRunnerForTest = fn
}

// Chain helpers re-exported for cli_test. These are pure functions
// in chain.go that benefit from direct testing rather than cobra
// driving (cobra adds friction for table-driven tests of, e.g.,
// `looksLikeIdent`).

// ParseVarFlagsForTest exposes parseVarFlags.
func ParseVarFlagsForTest(in []string) (map[string]string, error) {
	return parseVarFlags(in)
}

// LooksLikeIdentForTest exposes looksLikeIdent.
func LooksLikeIdentForTest(s string) bool { return looksLikeIdent(s) }

// ChainExitFromStatusForTest exposes chainExitFromStatus.
func ChainExitFromStatusForTest(res *chain.Result) error {
	return chainExitFromStatus(res)
}

// PrettyChainListForTest exposes prettyChainList.
func PrettyChainListForTest(w io.Writer, data any) error {
	return prettyChainList(w, data)
}

// ResolveStateDirForTest exposes resolveStateDir.
func ResolveStateDirForTest() (string, error) {
	return resolveStateDir()
}
