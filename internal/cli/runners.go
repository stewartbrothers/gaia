package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newRunnersCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runners",
		Short: "Inspect self-hosted Actions runners (status, busy, labels)",
	}
	cmd.AddCommand(newRunnersListCmd(flags))
	return cmd
}

func newRunnersListCmd(flags *globalFlags) *cobra.Command {
	var org bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List self-hosted Actions runners with status and labels",
		Long: `List the self-hosted Actions runners registered on the repo, or the
owner's org-level runners with --org.

Answers "is this CI runner online, is it busy, and what labels can it
run" — e.g. to confirm a deploy runner is live before triggering a
release. The repo-level list may be empty when runners are registered at
the org or instance level; use --org in that case.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildRunnersOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			runners, page, err := ops.ListRunners(cmd.Context(), owner, repo, provider.ListRunnersOptions{
				Org:    org,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, runners, page, prettyRunnerList)
		},
	}
	cmd.Flags().BoolVar(&org, "org", false, "list the owner's org-level runners instead of the repo's")
	return cmd
}

func prettyRunnerList(w io.Writer, data any) error {
	runners, ok := data.([]types.Runner)
	if !ok {
		return fmt.Errorf("prettyRunnerList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tSTATUS\tBUSY\tLABELS")
	for _, r := range runners {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Name, r.Status, strconv.FormatBool(r.Busy), strings.Join(r.Labels, ","))
	}
	return tw.Flush()
}
