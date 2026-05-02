package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newIssueCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "List, view, and search issues",
	}
	cmd.AddCommand(newIssueListCmd(flags))
	cmd.AddCommand(newIssueViewCmd(flags))
	return cmd
}

type issueListOptions struct {
	State    string
	Label    []string
	Assignee string
	Author   string
	Since    string
	Query    string
}

func newIssueListCmd(flags *globalFlags) *cobra.Command {
	var opts issueListOptions

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			po := provider.ListIssuesOptions{
				State:    opts.State,
				Labels:   opts.Label,
				Assignee: opts.Assignee,
				Author:   opts.Author,
				Query:    opts.Query,
				Limit:    flags.Limit,
				Cursor:   flags.Cursor,
			}
			if opts.Since != "" {
				t, perr := time.Parse(time.RFC3339, opts.Since)
				if perr != nil {
					return fmt.Errorf("--since: %w (expected RFC3339, e.g. 2026-01-01T00:00:00Z)", perr)
				}
				po.Since = t
			}
			issues, page, err := p.ListIssues(cmd.Context(), owner, repo, po)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issues, page, prettyIssueList)
		},
	}
	cmd.Flags().StringVar(&opts.State, "state", "", "filter by state: open, closed, all")
	cmd.Flags().StringSliceVar(&opts.Label, "label", nil, "filter by label (repeatable)")
	cmd.Flags().StringVar(&opts.Assignee, "assignee", "", "filter by assignee login")
	cmd.Flags().StringVar(&opts.Author, "author", "", "filter by author login")
	cmd.Flags().StringVar(&opts.Since, "since", "", "filter to updated after RFC3339 timestamp")
	cmd.Flags().StringVarP(&opts.Query, "query", "q", "", "search query")
	return cmd
}

func newIssueViewCmd(flags *globalFlags) *cobra.Command {
	var withComments int

	cmd := &cobra.Command{
		Use:   "view <number>",
		Short: "View an issue",
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
			issue, err := p.GetIssue(cmd.Context(), owner, repo, n, provider.GetIssueOptions{
				WithComments: withComments,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issue, nil, prettyIssueView)
		},
	}
	cmd.Flags().IntVar(&withComments, "with-comments", 0, "inline this many recent comments (0 = none)")
	return cmd
}

func prettyIssueList(w io.Writer, data any) error {
	issues, ok := data.([]types.Issue)
	if !ok {
		return fmt.Errorf("prettyIssueList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NUMBER\tSTATE\tTITLE\tAUTHOR\tLABELS")
	for _, i := range issues {
		labels := joinLabelNames(i.Labels)
		_, _ = fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\n", i.Number, i.State, truncate(i.Title, 60), i.Author.Login, labels)
	}
	return tw.Flush()
}

func prettyIssueView(w io.Writer, data any) error {
	issue, ok := data.(*types.Issue)
	if !ok {
		return fmt.Errorf("prettyIssueView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "#%d %s\n", issue.Number, issue.Title)
	_, _ = fmt.Fprintf(w, "State: %s   Author: %s\n", issue.State, issue.Author.Login)
	if len(issue.Labels) > 0 {
		_, _ = fmt.Fprintf(w, "Labels: %s\n", joinLabelNames(issue.Labels))
	}
	if issue.Body != "" {
		_, _ = fmt.Fprintf(w, "\n%s\n", issue.Body)
	}
	if len(issue.Comments) > 0 {
		_, _ = fmt.Fprintln(w, "\n--- Comments ---")
		for _, c := range issue.Comments {
			_, _ = fmt.Fprintf(w, "\n@%s on %s:\n%s\n", c.Author.Login, c.CreatedAt.Format("2006-01-02"), c.Body)
		}
	}
	return nil
}

func joinLabelNames(labels []types.Label) string {
	if len(labels) == 0 {
		return "-"
	}
	out := ""
	for i, l := range labels {
		if i > 0 {
			out += ", "
		}
		out += l.Name
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func parseIssueNumber(s string) (int, error) {
	// Accept "42" or "#42".
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	n, err := atoiStrict(s)
	if err != nil {
		return 0, fmt.Errorf("invalid issue number %q: %w", s, err)
	}
	return n, nil
}

func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("non-digit %q", c)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
