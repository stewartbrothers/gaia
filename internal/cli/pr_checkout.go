package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// gitRunner is the indirection that lets tests substitute a fake
// subprocess. Production callers use execGit.
type gitRunner func(ctx context.Context, dir string, args ...string) error

func execGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return exitcode.Wrap(
			fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out))),
			exitcode.Generic, "git",
		)
	}
	return nil
}

// gitRunnerForTest is set by export_test.go's hook so tests can pin a
// fake subprocess. nil means use execGit (production).
var gitRunnerForTest gitRunner

func runGit(ctx context.Context, dir string, args ...string) error {
	if gitRunnerForTest != nil {
		return gitRunnerForTest(ctx, dir, args...)
	}
	return execGit(ctx, dir, args...)
}

// checkoutPlan is the data the actual subprocess invocations need.
// Pulled out as a struct so tests can assert command construction
// without running git.
type checkoutPlan struct {
	HeadRef     string // refs/pull/<N>/head
	LocalBranch string // pr-<N>
	HeadSHA     string // for --detach mode
	Detach      bool
}

func planCheckout(pr *types.PullRequest, n int, detach bool) checkoutPlan {
	return checkoutPlan{
		HeadRef:     fmt.Sprintf("refs/pull/%d/head", n),
		LocalBranch: fmt.Sprintf("pr-%d", n),
		HeadSHA:     pr.Head.SHA,
		Detach:      detach,
	}
}

func newPRCheckoutCmd(flags *globalFlags) *cobra.Command {
	var detach bool
	cmd := &cobra.Command{
		Use:   "checkout <number>",
		Short: "Fetch a PR's head and check it out locally",
		Long: `Fetches refs/pull/<N>/head and checks it out as a local branch
"pr-<N>" (same convention as gh's checkout). With --detach, the
checkout lands at the head SHA without creating a branch.

Forgejo populates refs/pull/<N>/head for both same-repo and
cross-fork PRs, so this command works uniformly across both.

Refuses to overwrite local changes — git reports "your local
changes would be overwritten" and the command exits non-zero.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
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
			pr, err := p.GetPullRequest(cmd.Context(), owner, repo, n, provider.GetPullRequestOptions{})
			if err != nil {
				return err
			}
			plan := planCheckout(pr, n, detach)

			// Always fetch first; that pulls the ref into the local repo.
			if err := runGit(cmd.Context(), "", "fetch", "origin", plan.HeadRef+":"+plan.LocalBranch); err != nil {
				return err
			}
			if plan.Detach {
				if err := runGit(cmd.Context(), "", "checkout", "--detach", plan.HeadSHA); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Checked out PR #%d at %s (detached)\n", n, plan.HeadSHA)
			} else {
				if err := runGit(cmd.Context(), "", "checkout", plan.LocalBranch); err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Checked out PR #%d on branch %s\n", n, plan.LocalBranch)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "checkout at head SHA without creating a local branch")
	return cmd
}
