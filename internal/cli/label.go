package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newLabelCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "List, create, edit, and delete labels",
	}
	cmd.AddCommand(newLabelListCmd(flags))
	cmd.AddCommand(newLabelCreateCmd(flags))
	cmd.AddCommand(newLabelEditCmd(flags))
	cmd.AddCommand(newLabelDeleteCmd(flags))
	return cmd
}

func newLabelListCmd(flags *globalFlags) *cobra.Command {
	var nameFilter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List repo labels (optionally filtered by --name substring)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			labels, err := p.ListLabels(cmd.Context(), owner, repo, provider.ListLabelsOptions{
				Name: nameFilter,
			})
			if err != nil {
				return err
			}
			if flags.Format == "ndjson" {
				// ListLabels is single-shot (no pagination on
				// either forge), so just emit one fake "page"
				// through the streaming helper and let the
				// trailer wrap up.
				return renderListStreaming(cmd, flags, func(_ string) ([]any, *provider.Page, error) {
					return toAnySlice(labels), &provider.Page{}, nil
				})
			}
			return renderEnvelope(cmd, flags, labels, nil, prettyLabelList)
		},
	}
	cmd.Flags().StringVar(&nameFilter, "name", "", "case-insensitive substring filter on label name (#328)")
	return cmd
}

func newLabelCreateCmd(flags *globalFlags) *cobra.Command {
	var (
		name, color, desc string
		dryRun            bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a label",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return exitcode.Errorf(exitcode.Usage, "--name is required")
			}
			if color == "" {
				return exitcode.Errorf(exitcode.Usage, "--color is required (hex string without #)")
			}
			opts := provider.CreateLabelOptions{Name: name, Color: color, Description: desc}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/labels", owner, repo), opts)
			}
			lab, err := p.CreateLabel(cmd.Context(), owner, repo, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, lab, nil, prettyLabelOne)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "label name (required)")
	cmd.Flags().StringVar(&color, "color", "", "hex color without leading # (required)")
	cmd.Flags().StringVar(&desc, "description", "", "optional description")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request without posting")
	return cmd
}

func newLabelEditCmd(flags *globalFlags) *cobra.Command {
	var (
		newName, color, desc string
		dryRun               bool
	)
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a label by current name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := provider.EditLabelOptions{NewName: newName, Color: color, Description: desc}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("PATCH /repos/%s/%s/labels (by name=%q)", owner, repo, args[0]), opts)
			}
			lab, err := p.EditLabel(cmd.Context(), owner, repo, args[0], opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, lab, nil, prettyLabelOne)
		},
	}
	cmd.Flags().StringVar(&newName, "rename", "", "rename to this name")
	cmd.Flags().StringVar(&color, "color", "", "new hex color")
	cmd.Flags().StringVar(&desc, "description", "", "new description")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request without patching")
	return cmd
}

func newLabelDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a label by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete label %q. Re-run with --confirm to actually remove.\n", args[0])
				return nil
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := p.DeleteLabel(cmd.Context(), owner, repo, args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted label %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete the label (without this, prints what would happen)")
	return cmd
}

func prettyLabelList(w io.Writer, data any) error {
	labels, ok := data.([]types.Label)
	if !ok {
		return fmt.Errorf("prettyLabelList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tCOLOR\tDESCRIPTION")
	for _, l := range labels {
		desc := l.Description
		if desc == "" {
			desc = "-"
		}
		color := l.Color
		if color == "" {
			color = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", l.Name, color, desc)
	}
	return tw.Flush()
}

func prettyLabelOne(w io.Writer, data any) error {
	l, ok := data.(*types.Label)
	if !ok {
		return fmt.Errorf("prettyLabelOne: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "%s\n", l.Name)
	if l.Color != "" {
		_, _ = fmt.Fprintf(w, "  color: %s\n", l.Color)
	}
	if l.Description != "" {
		_, _ = fmt.Fprintf(w, "  description: %s\n", l.Description)
	}
	return nil
}
