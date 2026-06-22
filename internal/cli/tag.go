package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newTagCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "List, create, and delete git tags",
		Long: `List, create, and delete git tags.

Tags are bare git refs, independent of releases — ` + "`gaia tag`" + ` can list
a tag that has no release attached, and create or delete a tag without
touching the release surface. Use ` + "`gaia release`" + ` when you want the
notes-and-assets object instead.`,
	}
	cmd.AddCommand(newTagListCmd(flags))
	cmd.AddCommand(newTagCreateCmd(flags))
	cmd.AddCommand(newTagDeleteCmd(flags))
	return cmd
}

func newTagListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the repository's tags",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildTagOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			tags, page, err := ops.ListTags(cmd.Context(), owner, repo, provider.ListTagsOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, tags, page, prettyTagList)
		},
	}
}

func newTagCreateCmd(flags *globalFlags) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a tag from --from (or the repo's default branch)",
		Long: `Create a tag.

--from accepts a branch name, tag, or commit; when omitted the new tag
points at the tip of the repository's default branch.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ops, err := buildTagOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			tag, err := ops.CreateTag(cmd.Context(), owner, repo, args[0], provider.CreateTagOptions{From: from})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, tag, nil, prettyTag)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source ref to tag (branch, tag, or commit; default: the repo's default branch)")
	return cmd
}

func newTagDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete tag %q. Re-run with --confirm to actually remove.\n", args[0])
				return nil
			}
			ops, err := buildTagOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := ops.DeleteTag(cmd.Context(), owner, repo, args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted tag %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete the tag (without this, prints what would happen)")
	return cmd
}

func prettyTag(w io.Writer, data any) error {
	tag, ok := data.(*types.Tag)
	if !ok {
		return fmt.Errorf("prettyTag: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "Tag: %s\n", tag.Name)
	if tag.Commit != "" {
		_, _ = fmt.Fprintf(w, "Commit: %s\n", tag.Commit)
	}
	if tag.Message != "" {
		_, _ = fmt.Fprintf(w, "Message: %s\n", tag.Message)
	}
	return nil
}

func prettyTagList(w io.Writer, data any) error {
	tags, ok := data.([]types.Tag)
	if !ok {
		return fmt.Errorf("prettyTagList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tCOMMIT\tMESSAGE")
	for _, tag := range tags {
		sha := tag.Commit
		if len(sha) > 12 {
			sha = sha[:12]
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", tag.Name, sha, tag.Message)
	}
	return tw.Flush()
}
