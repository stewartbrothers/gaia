package cli

import (
	"context"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/github"
)

// githubAPIURL is the production GitHub.com API base. A package-level
// var (not a const) so tests can swap it for an httptest server via
// SetGithubAPIURLForTest in export_test.go. Phase 2 may also use this
// for GitHub Enterprise hosts; until then it stays hardcoded.
var githubAPIURL = "https://api.github.com"

func newAuthGHCmd() *cobra.Command {
	var project bool
	var noGitignore bool

	cmd := &cobra.Command{
		Use:   "gh",
		Short: "Authenticate to github.com (paste a PAT)",
		Long: `Prompts for a GitHub Personal Access Token, validates it via
GET /user against api.github.com, and records the credential. After
this, gaia commands targeting github.com work without env vars.

Phase 1 shipped paste-a-PAT only; Phase 2 will add OAuth Device Flow
(` + "`gaia auth gh --device`" + `) once a public OAuth app is registered;
this command's flags are forward-compatible with that.

Visit https://github.com/settings/tokens?type=beta to create a
fine-grained Personal Access Token. Recommended permissions:
Contents: read; Issues: read+write; Pull requests: read+write.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "GitHub (api.github.com)")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Visit https://github.com/settings/tokens?type=beta to create a")
			_, _ = fmt.Fprintln(out, "fine-grained Personal Access Token. Recommended permissions:")
			_, _ = fmt.Fprintln(out, "  - Contents: read")
			_, _ = fmt.Fprintln(out, "  - Issues: read+write")
			_, _ = fmt.Fprintln(out, "  - Pull requests: read+write")
			_, _ = fmt.Fprintln(out)

			token, err := readSecret(cmd.InOrStdin(), out, "Paste token: ")
			if err != nil {
				return exitcode.Wrap(err, exitcode.Generic, "read token")
			}
			if token == "" {
				return exitcode.Errorf(exitcode.Usage, "no token provided")
			}

			login, err := validateGitHubToken(cmd.Context(), githubAPIURL, token)
			if err != nil {
				return err
			}

			parsed, _ := url.Parse(githubAPIURL)
			cred := auth.Credential{
				APIURL: githubAPIURL,
				Token:  token,
				User:   login,
			}

			path, err := authStorePath(project)
			if err != nil {
				return err
			}
			store, err := auth.Load(path)
			if err != nil {
				return err
			}
			store.Set("github", parsed.Host, cred)
			if err := auth.Save(path, store); err != nil {
				return err
			}

			scope := "global"
			if project {
				scope = "project"
				if !noGitignore {
					if root := auth.ProjectRoot("."); root != "" {
						if err := auth.EnsureGitignored(root, ".gaia/credentials.yaml"); err != nil {
							_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to update .gitignore: %v\n", err)
						}
					}
				}
			}

			_, _ = fmt.Fprintf(out, "✓ Authenticated as %s\n✓ Saved to %s (%s)\n", login, path, scope)
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "save to .gaia/credentials.yaml in the current repo")
	cmd.Flags().BoolVar(&noGitignore, "no-gitignore", false, "skip auto-gitignoring .gaia/credentials.yaml on --project")
	return cmd
}

// validateGitHubToken validates a token by calling Whoami via the
// real github.Provider. Replaces the one-shot HTTP call that was
// here when core/github didn't exist (#31). The provider's auth
// header, retry logic, and error mapping all kick in for free —
// behavior is identical to a regular `gaia whoami` call against
// github except the provider construction is local to this command
// (we can't go through forgebuilder because no credentials are
// stored yet — that's the whole point of `gaia auth gh`).
func validateGitHubToken(ctx context.Context, baseURL, token string) (string, error) {
	p := github.NewProvider(github.Options{
		BaseURL: baseURL,
		Token:   token,
	})
	return p.Whoami(ctx)
}
