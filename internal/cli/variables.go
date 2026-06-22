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

func newVariablesCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "variables",
		Short: "Inspect configured CI/Actions variables (names + values + timestamps)",
	}
	cmd.AddCommand(newVariablesListCmd(flags))
	return cmd
}

func newVariablesListCmd(flags *globalFlags) *cobra.Command {
	var org bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured Actions variables (names + values + timestamps)",
		Long: `List the Actions variables configured on the repo, or the owner's
org-level variables with --org.

Unlike secrets, variable VALUES are non-secret config (e.g. TURBO_TEAM,
TURBO_API) and ARE returned by both forges' APIs — this answers "what
variables are set up here and to what".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildVariablesOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			variables, page, err := ops.ListVariables(cmd.Context(), owner, repo, provider.ListVariablesOptions{
				Org:    org,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, variables, page, prettyVariableList)
		},
	}
	cmd.Flags().BoolVar(&org, "org", false, "list the owner's org-level variables instead of the repo's")
	return cmd
}

func prettyVariableList(w io.Writer, data any) error {
	variables, ok := data.([]types.Variable)
	if !ok {
		return fmt.Errorf("prettyVariableList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tVALUE\tUPDATED")
	for _, v := range variables {
		ts := "-"
		switch {
		case v.UpdatedAt != nil:
			ts = v.UpdatedAt.Format(time.RFC3339)
		case v.CreatedAt != nil:
			ts = v.CreatedAt.Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Name, v.Value, ts)
	}
	return tw.Flush()
}
