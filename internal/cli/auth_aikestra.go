package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/aikestra"
)

// newAuthAikestraCmd is `gaia auth aikestra <api-url>` (#404). Aikestra's
// git origin and API origin can differ (dev: :4100 vs :3000), so the API
// URL is recorded per host here rather than derived from a clone URL.
func newAuthAikestraCmd() *cobra.Command {
	return newSelfHostedAuthCmd(selfHostedAuth{
		provider: "aikestra",
		display:  "Aikestra",
		long: `Prompts for an Aikestra access token, validates it against the given
instance's API (GET /api/v1/user), and records the credential under the
API host. Pass the web/API origin (e.g. https://aikestra.example or
http://127.0.0.1:3000), not the git clone origin — they can differ.

Mint a token under Account → Access tokens. A Delegated Agent credential
(an agent acting for you) works too and is preferred for agents; gaia
records the User's email either way.`,
		tokenHelp: func(origin string) string {
			return fmt.Sprintf("Visit %s and open Account → Access tokens to create\na token.", origin)
		},
		whoami: func(ctx context.Context, apiURL, token string) (string, error) {
			return aikestra.NewProvider(aikestra.Options{BaseURL: apiURL, Token: token}).Whoami(ctx)
		},
	})
}
