package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/forgejo"
)

func newAuthForgejoCmd() *cobra.Command {
	return newSelfHostedAuthCmd(selfHostedAuth{
		provider: "forgejo",
		display:  "Forgejo",
		long: `Prompts for a Personal Access Token, validates it against the given
Forgejo URL, and records the credential. After this, gaia commands
against this host work without env vars or flags.

Visit <url>/user/settings/applications to create a Personal Access
Token. Recommended scopes: read:repository, write:issue, read:user.`,
		tokenHelp: func(origin string) string {
			return fmt.Sprintf("Visit %s/user/settings/applications to create a\nPersonal Access Token.", origin)
		},
		whoami: func(ctx context.Context, apiURL, token string) (string, error) {
			return forgejo.NewProvider(forgejo.Options{BaseURL: apiURL, Token: token}).Whoami(ctx)
		},
	})
}

// selfHostedAuth describes a `gaia auth <forge> <url>` command for a
// forge with no universal endpoint: the operator names the instance, and
// the credential is keyed by its API host.
type selfHostedAuth struct {
	provider string // credential-store key, e.g. "forgejo"
	display  string // human name, e.g. "Forgejo"
	long     string // help text, before the shared --project paragraph
	// tokenHelp says where to mint a token, given the instance origin
	// (the API URL without /api/v1).
	tokenHelp func(origin string) string
	// whoami validates the token and returns the identity to record.
	whoami func(ctx context.Context, apiURL, token string) (string, error)
}

func newSelfHostedAuthCmd(spec selfHostedAuth) *cobra.Command {
	var project bool
	var noGitignore bool

	cmd := &cobra.Command{
		Use:   spec.provider + " <url>",
		Short: "Authenticate to a " + spec.display + " instance",
		Long: spec.long + `

By default the credential is saved globally
(~/.config/gaia/credentials.yaml). With --project, it is saved to
.gaia/credentials.yaml inside the current repo and .gaia/credentials.yaml
is added to the repo's .gitignore.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawURL := args[0]
			apiURL := normalizeForgejoURL(rawURL)
			parsed, err := url.Parse(apiURL)
			if err != nil || parsed.Host == "" {
				return exitcode.Errorf(exitcode.Usage, "invalid URL %q", rawURL)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s at %s\n\n", spec.display, apiURL)
			_, _ = fmt.Fprintf(out, "%s\n\n", spec.tokenHelp(strings.TrimSuffix(apiURL, "/api/v1")))

			token, err := readSecret(cmd.InOrStdin(), out, "Paste token: ")
			if err != nil {
				return exitcode.Wrap(err, exitcode.Generic, "read token")
			}
			if token == "" {
				return exitcode.Errorf(exitcode.Usage, "no token provided")
			}

			// Validate BEFORE persisting — invalid tokens never get saved.
			login, err := spec.whoami(cmd.Context(), apiURL, token)
			if err != nil {
				return err
			}

			cred := auth.Credential{
				APIURL: apiURL,
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
			store.Set(spec.provider, parsed.Host, cred)
			if err := auth.Save(path, store); err != nil {
				return err
			}

			scope := "global"
			if project {
				scope = "project"
				if !noGitignore {
					repoRoot := auth.ProjectRoot(".")
					if repoRoot != "" {
						if err := auth.EnsureGitignored(repoRoot, ".gaia/credentials.yaml"); err != nil {
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

// normalizeForgejoURL trims trailing slashes and ensures the URL ends
// with /api/v1. Forgejo's (and Aikestra's) API base is always at that
// path; users may pass either the bare instance URL or the full api/v1
// form.
func normalizeForgejoURL(raw string) string {
	s := strings.TrimRight(raw, "/")
	if !strings.HasSuffix(s, "/api/v1") {
		s += "/api/v1"
	}
	return s
}

func authStorePath(project bool) (string, error) {
	if !project {
		return auth.DefaultGlobalPath()
	}
	repoRoot := auth.ProjectRoot(".")
	if repoRoot == "" {
		return "", exitcode.Errorf(exitcode.Usage, "--project requires being inside a git repository")
	}
	return auth.ProjectPath(repoRoot), nil
}
