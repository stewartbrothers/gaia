package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newBranchCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch",
		Short: "Manage branch settings (protection rules)",
	}
	cmd.AddCommand(newBranchProtectionCmd(flags))
	return cmd
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
