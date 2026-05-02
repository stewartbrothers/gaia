package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
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

Phase 1 ships paste-a-PAT only. Phase 2 will add OAuth Device Flow
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

// validateGitHubToken issues a GET /user to baseURL with token, returns
// the login. Uses Bearer auth (works for both classic and fine-grained
// PATs; GitHub's docs prefer Bearer for all token types).
//
// We don't have a github provider yet (#31), so this is a one-shot
// HTTP call rather than a full provider. Phase 2 will replace this
// with core/github.Provider.Whoami once that lands.
func validateGitHubToken(ctx context.Context, baseURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/user", nil)
	if err != nil {
		return "", exitcode.Wrap(err, exitcode.Generic, "build GitHub /user request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", exitcode.Wrap(err, exitcode.Network, "GET /user")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", exitcode.Wrap(
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)),
			exitcode.FromHTTP(resp.StatusCode),
			"GET /user",
		)
	}

	var u struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", exitcode.Wrap(err, exitcode.Generic, "decode /user response")
	}
	return u.Login, nil
}
