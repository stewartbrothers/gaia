package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// readBody reads the body value supplied via a --body flag. The
// literal "-" means "read all of stdin"; any other value is returned
// as-is. Empty stdin is rejected so a blank body never accidentally
// gets posted.
func readBody(in io.Reader, raw string) (string, error) {
	if raw != "-" {
		return raw, nil
	}
	all, err := io.ReadAll(in)
	if err != nil {
		return "", exitcode.Wrap(err, exitcode.Generic, "read body from stdin")
	}
	if strings.TrimSpace(string(all)) == "" {
		return "", exitcode.Errorf(exitcode.Usage, "empty body from stdin")
	}
	return string(all), nil
}

// printDryRun emits an HTTP-method/path label followed by the JSON
// body that would be sent. Used by every write subcommand's --dry-run.
func printDryRun(cmd *cobra.Command, label string, body any) error {
	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(w, label)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(body)
}

func newIssueCreateCmd(flags *globalFlags) *cobra.Command {
	var (
		title   string
		body    string
		labels  []string
		assigns []string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a new issue",
		Long: `Creates a new issue. --title is required; --body may be passed
inline, omitted, or "-" to read from stdin. --dry-run prints the
request body without making the call.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if title == "" {
				return exitcode.Errorf(exitcode.Usage, "--title is required")
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
			opts := provider.CreateIssueOptions{
				Title:     title,
				Body:      b,
				Labels:    labels,
				Assignees: assigns,
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/issues", owner, repo), opts)
			}
			issue, err := p.CreateIssue(cmd.Context(), owner, repo, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issue, nil, prettyIssueView)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "issue title (required)")
	cmd.Flags().StringVar(&body, "body", "", "issue body, or \"-\" for stdin")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "label name (repeatable)")
	cmd.Flags().StringSliceVar(&assigns, "assignee", nil, "assignee login (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without posting")
	return cmd
}

func newIssueEditCmd(flags *globalFlags) *cobra.Command {
	var (
		title   string
		body    string
		state   string
		assigns []string
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "edit <number>",
		Short: "Edit an existing issue (title/body/state/assignees)",
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
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			opts := provider.EditIssueOptions{
				Title:     title,
				Body:      b,
				State:     state,
				Assignees: assigns,
			}
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("PATCH /repos/%s/%s/issues/%d", owner, repo, n), opts)
			}
			issue, err := p.EditIssue(cmd.Context(), owner, repo, n, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issue, nil, prettyIssueView)
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title (empty = no change)")
	cmd.Flags().StringVar(&body, "body", "", "new body, or \"-\" for stdin (empty = no change)")
	cmd.Flags().StringVar(&state, "state", "", "new state: open or closed (empty = no change)")
	cmd.Flags().StringSliceVar(&assigns, "assignee", nil, "replace assignees with these logins")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without patching")
	return cmd
}

func newIssueCloseCmd(flags *globalFlags) *cobra.Command {
	return newIssueStateCmd(flags, "close", "closed", "Close an issue")
}

func newIssueReopenCmd(flags *globalFlags) *cobra.Command {
	return newIssueStateCmd(flags, "reopen", "open", "Reopen an issue")
}

func newIssueStateCmd(flags *globalFlags, name, state, short string) *cobra.Command {
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
			issue, err := p.EditIssue(cmd.Context(), owner, repo, n, provider.EditIssueOptions{State: state})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issue, nil, prettyIssueView)
		},
	}
}

func newIssueCommentCmd(flags *globalFlags) *cobra.Command {
	var body string
	cmd := &cobra.Command{
		Use:   "comment <number>",
		Short: "Post a top-level comment on an issue or PR",
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

func newIssueCommentEditCmd(flags *globalFlags) *cobra.Command {
	var body string
	cmd := &cobra.Command{
		Use:   "comment-edit <comment-id>",
		Short: "Edit an existing comment by its ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return exitcode.Errorf(exitcode.Usage, "comment-id must be a number; got %q", args[0])
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
			c, err := p.EditIssueComment(cmd.Context(), owner, repo, id, b)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, c, nil, prettyComment)
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "new comment body, or \"-\" for stdin")
	return cmd
}

func newIssueCommentDeleteCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "comment-delete <comment-id>",
		Short: "Delete a comment by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return exitcode.Errorf(exitcode.Usage, "comment-id must be a number; got %q", args[0])
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if err := p.DeleteIssueComment(cmd.Context(), owner, repo, id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted comment %d\n", id)
			return nil
		},
	}
}

// prettyComment renders a single Comment for the comment subcommands.
func prettyComment(w io.Writer, data any) error {
	c, ok := data.(*types.Comment)
	if !ok {
		return fmt.Errorf("prettyComment: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "[%s] @%s on %s:\n%s\n",
		c.Source, c.Author.Login, c.CreatedAt.Format("2006-01-02 15:04"), c.Body)
	return nil
}
