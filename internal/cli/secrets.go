package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newSecretsCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Inspect configured CI/Actions secrets (names + timestamps, never values)",
	}
	cmd.AddCommand(newSecretsListCmd(flags))
	return cmd
}

func newSecretsListCmd(flags *globalFlags) *cobra.Command {
	var org bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured Actions secrets (names + timestamps; values are never exposed)",
		Long: `List the Actions secrets configured on the repo, or the owner's
org-level secrets with --org.

Secret VALUES are write-only on both forges' APIs and are never returned;
this answers "which secrets are set up here" — e.g. to confirm a release
workflow's GORELEASER_TAP_DEPLOY_KEY / GH_RELEASE_TOKEN are present rather
than silently unset.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildSecretsOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			secrets, page, err := ops.ListSecrets(cmd.Context(), owner, repo, provider.ListSecretsOptions{
				Org:    org,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, secrets, page, prettySecretList)
		},
	}
	cmd.Flags().BoolVar(&org, "org", false, "list the owner's org-level secrets instead of the repo's")
	return cmd
}

func prettySecretList(w io.Writer, data any) error {
	secrets, ok := data.([]types.Secret)
	if !ok {
		return fmt.Errorf("prettySecretList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tUPDATED")
	for _, s := range secrets {
		ts := "-"
		switch {
		case s.UpdatedAt != nil:
			ts = s.UpdatedAt.Format(time.RFC3339)
		case s.CreatedAt != nil:
			ts = s.CreatedAt.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", s.Name, ts)
	}
	return tw.Flush()
}
