package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newBranchCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch",
		Short: "Manage branches and branch settings",
	}
	cmd.AddCommand(newBranchListCmd(flags))
	cmd.AddCommand(newBranchCreateCmd(flags))
	cmd.AddCommand(newBranchProtectionCmd(flags))
	return cmd
}

func newBranchListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the repository's branches",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ops, err := buildBranchOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			branches, page, err := ops.ListBranches(cmd.Context(), owner, repo, provider.ListBranchesOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, branches, page, prettyBranchList)
		},
	}
}

func newBranchCreateCmd(flags *globalFlags) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a branch from --from (or the repo's default branch)",
		Long: `Create a branch.

--from accepts a branch name, tag, or commit; when omitted the new branch
is cut from the repository's default branch.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ops, err := buildBranchOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			br, err := ops.CreateBranch(cmd.Context(), owner, repo, args[0], provider.CreateBranchOptions{From: from})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, br, nil, prettyBranch)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source ref to branch from (branch, tag, or commit; default: the repo's default branch)")
	return cmd
}

func prettyBranch(w io.Writer, data any) error {
	b, ok := data.(*types.Branch)
	if !ok {
		return fmt.Errorf("prettyBranch: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "Branch: %s\n", b.Name)
	if b.Commit != "" {
		_, _ = fmt.Fprintf(w, "Commit: %s\n", b.Commit)
	}
	if b.Protected {
		_, _ = fmt.Fprintln(w, "Protected: true")
	}
	return nil
}

func prettyBranchList(w io.Writer, data any) error {
	branches, ok := data.([]types.Branch)
	if !ok {
		return fmt.Errorf("prettyBranchList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tCOMMIT\tPROTECTED")
	for _, b := range branches {
		sha := b.Commit
		if len(sha) > 12 {
			sha = sha[:12]
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%v\n", b.Name, sha, b.Protected)
	}
	return tw.Flush()
}

func newBranchProtectionCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "protection",
		Short: "Get, set, or delete a branch's protection rule",
		Long: `Get, set, or delete a branch's protection rule.

The required status-check contexts are the binding part: a branch that
requires a named check can't merge while that check is red AND can't
merge while it is absent. Read the exact context strings with
` + "`gaia pr view <n> --with-ci`" + `.`,
	}
	cmd.AddCommand(newBranchProtectionGetCmd(flags))
	cmd.AddCommand(newBranchProtectionSetCmd(flags))
	cmd.AddCommand(newBranchProtectionDeleteCmd(flags))
	return cmd
}

func newBranchProtectionGetCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get <branch>",
		Short: "Show the protection rule for a branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ops, err := buildBranchProtectionOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			bp, err := ops.GetBranchProtection(cmd.Context(), owner, repo, args[0])
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, bp, nil, prettyBranchProtection)
		},
	}
}

func newBranchProtectionSetCmd(flags *globalFlags) *cobra.Command {
	var (
		requiredChecks []string
		strict         bool
		approvals      int
		dryRun         bool
	)
	cmd := &cobra.Command{
		Use:   "set <branch>",
		Short: "Create or replace the protection rule for a branch",
		Long: `Create or replace the protection rule for a branch.

Declarative: the flags fully specify the rule. Omitting --required-check
clears the required checks; omitting --required-approvals sets it to 0.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := provider.SetBranchProtectionOptions{
				RequiredStatusChecks: requiredChecks,
				StrictStatusChecks:   strict,
				RequiredApprovals:    approvals,
			}
			ops, err := buildBranchProtectionOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("set branch protection on %s/%s@%s", owner, repo, args[0]), opts)
			}
			bp, err := ops.SetBranchProtection(cmd.Context(), owner, repo, args[0], opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, bp, nil, prettyBranchProtection)
		},
	}
	cmd.Flags().StringSliceVar(&requiredChecks, "required-check", nil, "status-check context that must pass before merge (repeatable, e.g. --required-check 'CI / Build')")
	cmd.Flags().BoolVar(&strict, "strict", false, "require the branch to be up to date with base before merge")
	cmd.Flags().IntVar(&approvals, "required-approvals", 0, "number of approving reviews required to merge")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the desired rule without applying it")
	return cmd
}

func newBranchProtectionDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <branch>",
		Short: "Remove the protection rule for a branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete protection on branch %q. Re-run with --confirm to actually remove.\n", args[0])
				return nil
			}
			ops, err := buildBranchProtectionOps(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := ops.DeleteBranchProtection(cmd.Context(), owner, repo, args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted protection on branch %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete the protection rule (without this, prints what would happen)")
	return cmd
}

func prettyBranchProtection(w io.Writer, data any) error {
	bp, ok := data.(*types.BranchProtection)
	if !ok {
		return fmt.Errorf("prettyBranchProtection: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "Branch: %s\n", bp.Branch)
	if len(bp.RequiredStatusChecks) > 0 {
		_, _ = fmt.Fprintf(w, "Required checks (strict=%v):\n", bp.StrictStatusChecks)
		for _, c := range bp.RequiredStatusChecks {
			_, _ = fmt.Fprintf(w, "  %s\n", c)
		}
	} else {
		_, _ = fmt.Fprintln(w, "Required checks: none")
	}
	_, _ = fmt.Fprintf(w, "Required approvals: %d\n", bp.RequiredApprovals)
	return nil
}
