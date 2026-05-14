package cli

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newMilestoneCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "List, view, create, edit, and delete milestones",
	}
	cmd.AddCommand(newMilestoneListCmd(flags))
	cmd.AddCommand(newMilestoneViewCmd(flags))
	cmd.AddCommand(newMilestoneCreateCmd(flags))
	cmd.AddCommand(newMilestoneEditCmd(flags))
	cmd.AddCommand(newMilestoneDeleteCmd(flags))
	cmd.AddCommand(newMilestoneIssuesCmd(flags))
	return cmd
}

func newMilestoneListCmd(flags *globalFlags) *cobra.Command {
	var (
		state string
		name  string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			po := provider.ListMilestonesOptions{
				State:  state,
				Name:   name,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			}
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					ppo := po
					ppo.Cursor = cursor
					ms, page, err := p.ListMilestones(cmd.Context(), owner, repo, ppo)
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(ms), page, nil
				})
			}
			ms, page, err := p.ListMilestones(cmd.Context(), owner, repo, po)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, ms, page, prettyMilestoneList)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by state: open (default), closed, all")
	cmd.Flags().StringVar(&name, "name", "", "filter by title substring (Forgejo only; GitHub ignores)")
	return cmd
}

func newMilestoneViewCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "view <id>",
		Short: "View one milestone by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMilestoneID(args[0])
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			m, err := p.GetMilestone(cmd.Context(), owner, repo, id)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, m, nil, prettyMilestoneView)
		},
	}
}

func newMilestoneCreateCmd(flags *globalFlags) *cobra.Command {
	var (
		title       string
		description string
		due         string
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new milestone",
		Long: `Creates a new milestone on the active repo.

  $ gaia milestone create --title "Sprint 23" --description "May focus"
  $ gaia milestone create --title v0.5.0 --due 2026-06-01T00:00:00Z

--due accepts an RFC3339 timestamp. Leave it empty for no deadline.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if title == "" {
				return exitcode.Errorf(exitcode.Usage, "--title is required")
			}
			opts := provider.CreateMilestoneOptions{
				Title:       title,
				Description: description,
			}
			if due != "" {
				t, err := time.Parse(time.RFC3339, due)
				if err != nil {
					return exitcode.Errorf(exitcode.Usage,
						"--due: %v (expected RFC3339, e.g. 2026-06-01T00:00:00Z)", err)
				}
				opts.DueOn = &t
			}

			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/milestones", owner, repo), opts)
			}
			m, err := p.CreateMilestone(cmd.Context(), owner, repo, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, m, nil, prettyMilestoneView)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "milestone title (required)")
	cmd.Flags().StringVar(&description, "description", "", "optional description")
	cmd.Flags().StringVar(&due, "due", "", "RFC3339 due date, e.g. 2026-06-01T00:00:00Z")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request without posting")
	return cmd
}

func newMilestoneEditCmd(flags *globalFlags) *cobra.Command {
	var (
		title       string
		description string
		state       string
		due         string
		dryRun      bool
	)
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a milestone by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMilestoneID(args[0])
			if err != nil {
				return err
			}
			opts := provider.EditMilestoneOptions{
				Title:       title,
				Description: description,
				State:       state,
			}
			if due != "" {
				t, err := time.Parse(time.RFC3339, due)
				if err != nil {
					return exitcode.Errorf(exitcode.Usage,
						"--due: %v (expected RFC3339, e.g. 2026-06-01T00:00:00Z)", err)
				}
				opts.DueOn = &t
			}

			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("PATCH /repos/%s/%s/milestones/%d", owner, repo, id), opts)
			}
			m, err := p.EditMilestone(cmd.Context(), owner, repo, id, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, m, nil, prettyMilestoneView)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringVar(&state, "state", "", "new state: open or closed")
	cmd.Flags().StringVar(&due, "due", "", "new RFC3339 due date")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request without patching")
	return cmd
}

func newMilestoneDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a milestone by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMilestoneID(args[0])
			if err != nil {
				return err
			}
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete milestone %d. Re-run with --confirm to actually remove.\n", id)
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
			if err := p.DeleteMilestone(cmd.Context(), owner, repo, id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted milestone %d\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete (without this, prints what would happen)")
	return cmd
}

func newMilestoneIssuesCmd(flags *globalFlags) *cobra.Command {
	var state string
	cmd := &cobra.Command{
		Use:   "issues <id>",
		Short: "List issues attached to a milestone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMilestoneID(args[0])
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			po := provider.ListMilestoneIssuesOptions{
				State:  state,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			}
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					ppo := po
					ppo.Cursor = cursor
					issues, page, err := p.ListMilestoneIssues(cmd.Context(), owner, repo, id, ppo)
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(issues), page, nil
				})
			}
			issues, page, err := p.ListMilestoneIssues(cmd.Context(), owner, repo, id, po)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issues, page, prettyIssueList)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by issue state: open (default), closed, all")
	return cmd
}

// parseMilestoneID validates a positional <id> argument as a positive
// int64. Forgejo and GitHub both key milestones by integer ID (or
// Number on GitHub which we surface as ID); a non-numeric argument
// is a usage error rather than a NotFound from the forge.
func parseMilestoneID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, exitcode.Errorf(exitcode.Usage, "milestone id must be a positive integer; got %q", s)
	}
	return id, nil
}

func prettyMilestoneList(w io.Writer, data any) error {
	ms, ok := data.([]types.Milestone)
	if !ok {
		return fmt.Errorf("prettyMilestoneList: unexpected type %T", data)
	}
	if len(ms) == 0 {
		_, _ = fmt.Fprintln(w, "(no milestones)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tTITLE\tSTATE\tOPEN\tCLOSED\tDUE")
	for _, m := range ms {
		due := "-"
		if m.DueOn != nil {
			due = m.DueOn.Format("2006-01-02")
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\t%s\n",
			m.ID, truncate(m.Title, 40), m.State, m.OpenIssues, m.ClosedIssues, due)
	}
	return tw.Flush()
}

func prettyMilestoneView(w io.Writer, data any) error {
	m, ok := data.(*types.Milestone)
	if !ok {
		return fmt.Errorf("prettyMilestoneView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "#%d %s\n", m.ID, m.Title)
	_, _ = fmt.Fprintf(w, "  State:   %s\n", m.State)
	_, _ = fmt.Fprintf(w, "  Open:    %d issue(s)\n", m.OpenIssues)
	_, _ = fmt.Fprintf(w, "  Closed:  %d issue(s)\n", m.ClosedIssues)
	if m.DueOn != nil {
		_, _ = fmt.Fprintf(w, "  Due:     %s\n", m.DueOn.Format("2006-01-02"))
	}
	_, _ = fmt.Fprintf(w, "  Created: %s\n", m.CreatedAt.Format("2006-01-02 15:04"))
	if m.ClosedAt != nil {
		_, _ = fmt.Fprintf(w, "  Closed:  %s\n", m.ClosedAt.Format("2006-01-02 15:04"))
	}
	if m.Description != "" {
		_, _ = fmt.Fprintln(w)
		writeExternal(w, m.Description)
	}
	return nil
}
