package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newPRCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "List and view pull requests",
	}
	cmd.AddCommand(newPRListCmd(flags))
	cmd.AddCommand(newPRViewCmd(flags))
	cmd.AddCommand(newPRDiffCmd(flags))
	cmd.AddCommand(newPRCommentsCmd(flags))
	return cmd
}

type prListOptions struct {
	State string
	Label []string
	Head  string
	Base  string
}

func newPRListCmd(flags *globalFlags) *cobra.Command {
	var opts prListOptions

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			po := provider.ListPullRequestsOptions{
				State:  opts.State,
				Labels: opts.Label,
				Head:   opts.Head,
				Base:   opts.Base,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			}
			prs, page, err := p.ListPullRequests(cmd.Context(), owner, repo, po)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, prs, page, prettyPRList)
		},
	}
	cmd.Flags().StringVar(&opts.State, "state", "", "filter by state: open, closed, all")
	cmd.Flags().StringSliceVar(&opts.Label, "label", nil, "filter by label (repeatable)")
	cmd.Flags().StringVar(&opts.Head, "head", "", "filter by head ref")
	cmd.Flags().StringVar(&opts.Base, "base", "", "filter by base ref")
	return cmd
}

func newPRViewCmd(flags *globalFlags) *cobra.Command {
	var withComments int
	var withCI bool

	cmd := &cobra.Command{
		Use:   "view <number>",
		Short: "View a pull request",
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
			pr, err := p.GetPullRequest(cmd.Context(), owner, repo, n, provider.GetPullRequestOptions{
				WithComments:  withComments,
				WithCISummary: withCI,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pr, nil, prettyPRView)
		},
	}
	cmd.Flags().IntVar(&withComments, "with-comments", 0, "inline this many recent comments (0 = none)")
	cmd.Flags().BoolVar(&withCI, "with-ci", false, "fetch CI status for the PR's head commit")
	return cmd
}

func prettyPRList(w io.Writer, data any) error {
	prs, ok := data.([]types.PullRequest)
	if !ok {
		return fmt.Errorf("prettyPRList: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NUMBER\tSTATE\tTITLE\tAUTHOR\tHEAD\tBASE")
	for _, pr := range prs {
		_, _ = fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%s\n",
			pr.Number, pr.State, truncate(pr.Title, 50),
			pr.Author.Login, pr.Head.Ref, pr.Base.Ref)
	}
	return tw.Flush()
}

func prettyPRView(w io.Writer, data any) error {
	pr, ok := data.(*types.PullRequest)
	if !ok {
		return fmt.Errorf("prettyPRView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "#%d %s\n", pr.Number, pr.Title)
	_, _ = fmt.Fprintf(w, "State: %s   Author: %s   Draft: %v\n", pr.State, pr.Author.Login, pr.Draft)
	_, _ = fmt.Fprintf(w, "Head:  %s @ %s\n", pr.Head.Ref, pr.Head.SHA)
	_, _ = fmt.Fprintf(w, "Base:  %s @ %s\n", pr.Base.Ref, pr.Base.SHA)
	if pr.Mergeable != nil {
		_, _ = fmt.Fprintf(w, "Mergeable: %v\n", *pr.Mergeable)
	}
	if len(pr.Labels) > 0 {
		_, _ = fmt.Fprintf(w, "Labels: %s\n", joinLabelNames(pr.Labels))
	}
	if pr.CISummary != nil {
		_, _ = fmt.Fprintf(w, "CI: %s (%d successful / %d failed / %d pending out of %d)\n",
			pr.CISummary.State, pr.CISummary.Successful, pr.CISummary.Failed, pr.CISummary.Pending, pr.CISummary.Total)
	}
	if pr.Body != "" {
		_, _ = fmt.Fprintf(w, "\n%s\n", pr.Body)
	}
	if len(pr.Comments) > 0 {
		_, _ = fmt.Fprintln(w, "\n--- Comments ---")
		for _, c := range pr.Comments {
			_, _ = fmt.Fprintf(w, "\n@%s on %s:\n%s\n", c.Author.Login, c.CreatedAt.Format("2006-01-02"), c.Body)
		}
	}
	return nil
}
