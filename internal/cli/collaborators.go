package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newCollaboratorsCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collaborators",
		Short: "Inspect a repo's collaborator access list (who can touch this repo and at what level)",
	}
	cmd.AddCommand(newCollaboratorsListCmd(flags))
	return cmd
}

func newCollaboratorsListCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the repo's collaborators with their permission level",
		Long: `List the users with read-or-better access to the repo, each with
their effective permission level — an access audit of who can touch the
repo and at what level.

GitHub returns the permission inline; Forgejo's list omits it, so gaia
resolves each collaborator's permission with one extra per-user call.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildCollaboratorsOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			collaborators, page, err := ops.ListCollaborators(cmd.Context(), owner, repo, provider.ListCollaboratorsOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, collaborators, page, prettyCollaboratorList)
		},
	}
	return cmd
}

func prettyCollaboratorList(w io.Writer, data any) error {
	collaborators, ok := data.([]types.Collaborator)
	if !ok {
		return fmt.Errorf("prettyCollaboratorList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "LOGIN\tPERMISSION")
	for _, c := range collaborators {
		perm := c.Permission
		if perm == "" {
			perm = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", c.Login, perm)
	}
	return tw.Flush()
}
