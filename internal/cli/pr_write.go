package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

func newPRCreateCmd(flags *globalFlags) *cobra.Command {
	var (
		title  string
		body   string
		head   string
		base   string
		draft  bool
		labels []string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a new pull request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if title == "" {
				return exitcode.Errorf(exitcode.Usage, "--title is required")
			}
			if head == "" || base == "" {
				return exitcode.Errorf(exitcode.Usage, "--head and --base are both required")
			}
			b, err := readBody(cmd.InOrStdin(), body)
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
			opts := provider.CreatePullRequestOptions{
				Title:  title,
				Body:   b,
				Head:   head,
				Base:   base,
				Draft:  draft,
				Labels: labels,
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/pulls", owner, repo), opts)
			}
			pr, err := p.CreatePullRequest(cmd.Context(), owner, repo, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pr, nil, prettyPRView)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "PR title (required)")
	cmd.Flags().StringVar(&body, "body", "", "PR body, or \"-\" for stdin")
	cmd.Flags().StringVar(&head, "head", "", "head ref, e.g. feature/x or owner:feature/x for forks")
	cmd.Flags().StringVar(&base, "base", "main", "base ref")
	cmd.Flags().BoolVar(&draft, "draft", false, "open as draft")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label name (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without posting")
	return cmd
}

func newPREditCmd(flags *globalFlags) *cobra.Command {
	var (
		title  string
		body   string
		state  string
		draft  string // "true", "false", or "" (no change)
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "edit <number>",
		Short: "Edit a pull request (title/body/state/draft)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			b, err := readBody(cmd.InOrStdin(), body)
			if err != nil {
				return err
			}
			opts := provider.EditPullRequestOptions{
				Title: title,
				Body:  b,
				State: state,
			}
			switch strings.ToLower(strings.TrimSpace(draft)) {
			case "true", "1", "yes":
				v := true
				opts.Draft = &v
			case "false", "0", "no":
				v := false
				opts.Draft = &v
			case "":
				// no change
			default:
				return exitcode.Errorf(exitcode.Usage, "--draft must be true|false (or empty); got %q", draft)
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
				return printDryRun(cmd, fmt.Sprintf("PATCH /repos/%s/%s/pulls/%d", owner, repo, n), opts)
			}
			pr, err := p.EditPullRequest(cmd.Context(), owner, repo, n, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pr, nil, prettyPRView)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title (empty = no change)")
	cmd.Flags().StringVar(&body, "body", "", "new body, or \"-\" for stdin (empty = no change)")
	cmd.Flags().StringVar(&state, "state", "", "new state: open or closed (empty = no change)")
	cmd.Flags().StringVar(&draft, "draft", "", "true to mark draft, false to mark ready (empty = no change)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without patching")
	return cmd
}

func newPRCloseCmd(flags *globalFlags) *cobra.Command {
	return newPRStateCmd(flags, "close", "closed", "Close a pull request without merging")
}

func newPRReopenCmd(flags *globalFlags) *cobra.Command {
	return newPRStateCmd(flags, "reopen", "open", "Reopen a closed pull request")
}

func newPRStateCmd(flags *globalFlags, name, state, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <number>",
		Short: short,
		Args:  cobra.ExactArgs(1),
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
			pr, err := p.EditPullRequest(cmd.Context(), owner, repo, n, provider.EditPullRequestOptions{State: state})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pr, nil, prettyPRView)
		},
	}
}

// PR top-level comments share the issue-comment endpoint — the
// implementation just forwards to CreateIssueComment. Living under
// `pr` gives the discoverability of a `gaia pr comment 42` flow
// even though Forgejo treats them as issue comments internally.
func newPRCommentCmd(flags *globalFlags) *cobra.Command {
	var body string
	cmd := &cobra.Command{
		Use:   "comment-create <number>",
		Short: "Post a top-level thread comment on a PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			b, err := readBody(cmd.InOrStdin(), body)
			if err != nil {
				return err
			}
			if strings.TrimSpace(b) == "" {
				return exitcode.Errorf(exitcode.Usage, "--body is required (or \"-\" for stdin)")
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			c, err := p.CreateIssueComment(cmd.Context(), owner, repo, n, b)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, c, nil, prettyComment)
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "comment body, or \"-\" for stdin")
	return cmd
}

func newPRMergeCmd(flags *globalFlags) *cobra.Command {
	var (
		method       string
		title        string
		message      string
		deleteBranch bool
		dryRun       bool
	)
	cmd := &cobra.Command{
		Use:   "merge <number>",
		Short: "Merge a pull request",
		Long: `Merge a pull request via the configured provider.

Exits 0 on success. Failures map to structured exit codes so
chains can route on them via yield_on / abort_on:

  7   MergeConflict   — head ref diverged (HTTP 409)
  8   ReviewRequired  — branch protection needs more approvals (HTTP 405 + review-related body)
  9   PolicyViolation — branch protection blocked the merge for another reason
  3   NotFound        — PR or repo does not exist
  4   Auth            — credentials missing or rejected`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			opts := provider.MergePullRequestOptions{
				Method:       method,
				Title:        title,
				Message:      message,
				DeleteBranch: deleteBranch,
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
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/pulls/%d/merge", owner, repo, n), opts)
			}
			if err := p.MergePullRequest(cmd.Context(), owner, repo, n, opts); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Merged #%d using %q\n", n, defaultIfEmpty(method, "merge"))
			return nil
		},
	}
	cmd.Flags().StringVar(&method, "method", "merge", "merge method: merge|rebase|squash")
	cmd.Flags().StringVar(&title, "title", "", "merge commit title (squash/merge only)")
	cmd.Flags().StringVar(&message, "message", "", "merge commit message")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "delete the head branch after a successful merge")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without merging")
	return cmd
}

func defaultIfEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
