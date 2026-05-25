package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// newIssueDepCmd is the parent for `gaia issue dep` — list / add /
// remove issue-dependency relationships. Two directions exist
// (blockers = issues blocking this one; blocks = issues this one is
// blocking), but they describe the same edge — POST/DELETE on the
// dependency endpoint covers both via the inverse framing. See #317.
func newIssueDepCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "List, add, and remove issue dependencies (blockers + blocks)",
		Long: `Manage issue-dependency relationships.

Two directions exist:

  blockers — issues blocking this one (this issue depends on them)
  blocks   — issues this one is blocking (the inverse view)

"X blocks Y" and "Y depends on X" describe the same edge from
different framings. add/remove accept either --blocker or --blocks
and map both to the same underlying op.

Forgejo only; GitHub returns NotImplemented (no REST equivalent).`,
	}
	cmd.AddCommand(newIssueDepListCmd(flags))
	cmd.AddCommand(newIssueDepAddCmd(flags))
	cmd.AddCommand(newIssueDepRemoveCmd(flags))
	return cmd
}

func newIssueDepListCmd(flags *globalFlags) *cobra.Command {
	var direction string

	cmd := &cobra.Command{
		Use:   "list <number>",
		Short: "List the dependencies (blockers) or blocks of an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			if direction != "blockers" && direction != "blocks" {
				return exitcode.Errorf(exitcode.Usage,
					`--direction must be "blockers" or "blocks", got %q`, direction)
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			po := provider.ListIssueDepsOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			}
			var issues []types.Issue
			var page *provider.Page
			switch direction {
			case "blockers":
				issues, page, err = p.ListIssueDependencies(cmd.Context(), owner, repo, n, po)
			case "blocks":
				issues, page, err = p.ListIssueBlocks(cmd.Context(), owner, repo, n, po)
			}
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issues, page, prettyIssueList)
		},
	}
	cmd.Flags().StringVar(&direction, "direction", "blockers",
		`which side of the edge to list: "blockers" (default) or "blocks"`)
	return cmd
}

// newIssueDepAddCmd makes issue M block issue N (creates a
// dependency edge). The CLI accepts both framings via mutually-
// exclusive flags:
//
//	--blocker M  → "M blocks N" — POST .../N/dependencies {"index": M}
//	--blocks  M  → "N blocks M" — POST .../M/dependencies {"index": N}
//
// Same edge from different framings; the inverse is just the
// argument swap. Forgejo echoes the added blocker back as the
// response; we render it as a single-issue envelope.
func newIssueDepAddCmd(flags *globalFlags) *cobra.Command {
	var blocker, blocks int

	cmd := &cobra.Command{
		Use:   "add <number>",
		Short: "Add a dependency edge — either --blocker M (M blocks N) or --blocks M (N blocks M)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			depTarget, depHost, err := resolveDepDirection(n, blocker, blocks)
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
			added, err := p.AddIssueDependency(cmd.Context(), owner, repo, depHost, depTarget)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, added, nil, prettyIssueView)
		},
	}
	cmd.Flags().IntVar(&blocker, "blocker", 0,
		"issue number that blocks the argument issue (M blocks N)")
	cmd.Flags().IntVar(&blocks, "blocks", 0,
		"issue number that the argument issue blocks (N blocks M)")
	return cmd
}

func newIssueDepRemoveCmd(flags *globalFlags) *cobra.Command {
	var blocker, blocks int

	cmd := &cobra.Command{
		Use:   "remove <number>",
		Short: "Remove a dependency edge — same --blocker/--blocks shape as add",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			depTarget, depHost, err := resolveDepDirection(n, blocker, blocks)
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
			if err := p.RemoveIssueDependency(cmd.Context(), owner, repo, depHost, depTarget); err != nil {
				return err
			}
			// Mirror the milestone-delete shape: no body, just an empty
			// envelope so callers see a successful exit.
			return renderEnvelope(cmd, flags, struct{}{}, nil, prettyIssueDepRemoveOK)
		},
	}
	cmd.Flags().IntVar(&blocker, "blocker", 0,
		"issue number that blocks the argument issue (M blocks N)")
	cmd.Flags().IntVar(&blocks, "blocks", 0,
		"issue number that the argument issue blocks (N blocks M)")
	return cmd
}

// resolveDepDirection enforces the mutual exclusion of --blocker /
// --blocks and returns (depTarget, depHost) such that calling
// AddIssueDependency(host, target) creates the edge "target blocks
// host." The naming reads:
//
//   - --blocker M on issue N → "M blocks N." Edge stored on N's
//     /dependencies. Host=N, Target=M.
//   - --blocks M on issue N → "N blocks M." Same edge, framed from
//     the other side. Edge stored on M's /dependencies. Host=M,
//     Target=N.
//
// Exactly one of the two flags must be > 0.
func resolveDepDirection(n, blocker, blocks int) (target, host int, err error) {
	switch {
	case blocker > 0 && blocks > 0:
		return 0, 0, exitcode.Errorf(exitcode.Usage,
			"--blocker and --blocks are mutually exclusive")
	case blocker > 0:
		return blocker, n, nil
	case blocks > 0:
		return n, blocks, nil
	default:
		return 0, 0, exitcode.Errorf(exitcode.Usage,
			"one of --blocker or --blocks is required (issue number > 0)")
	}
}

func prettyIssueDepRemoveOK(w io.Writer, _ any) error {
	_, err := fmt.Fprintln(w, "dependency edge removed")
	return err
}
