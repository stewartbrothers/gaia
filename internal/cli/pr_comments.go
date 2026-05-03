package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newPRCommentsCmd(flags *globalFlags) *cobra.Command {
	var sources []string

	cmd := &cobra.Command{
		Use:   "comments <number>",
		Short: "List unified comments (issue + review + inline) for a PR",
		Long: `Returns the merged time-ordered comment stream for a PR, drawing
from up to three Forgejo endpoints (top-level thread, review records,
inline review comments). Each comment carries a Source discriminator
("issue" | "review" | "inline") and inline comments include
path + line.

Use --source to narrow: --source inline returns only the line-level
review remarks; --source issue,review returns everything except
inline.`,
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
			if flags.Format == "ndjson" {
				// ListComments returns the merged stream as a
				// single slice; there's no per-page cursor at the
				// provider boundary today (it reconciles 3
				// endpoints internally). Stream it through the
				// helper as one fake "page" so the wire shape
				// matches list commands that DO paginate.
				return renderListStreaming(cmd, flags, func(_ string) ([]any, *provider.Page, error) {
					comments, err := p.ListComments(cmd.Context(), owner, repo, n, provider.ListCommentsOptions{
						Sources: sources,
						Limit:   flags.Limit,
						Cursor:  flags.Cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(comments), &provider.Page{}, nil
				})
			}
			comments, err := p.ListComments(cmd.Context(), owner, repo, n, provider.ListCommentsOptions{
				Sources: sources,
				Limit:   flags.Limit,
				Cursor:  flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, comments, nil, prettyPRComments)
		},
	}
	cmd.Flags().StringSliceVar(&sources, "source", nil, "filter by source: issue, review, inline (repeatable; default = all)")
	return cmd
}

func prettyPRComments(w io.Writer, data any) error {
	comments, ok := data.([]types.Comment)
	if !ok {
		return fmt.Errorf("prettyPRComments: unexpected type %T", data)
	}
	if len(comments) == 0 {
		_, _ = fmt.Fprintln(w, "(no comments)")
		return nil
	}
	for i, c := range comments {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		header := fmt.Sprintf("[%s] @%s on %s",
			c.Source, c.Author.Login, c.CreatedAt.Format("2006-01-02 15:04"))
		if c.Source == "inline" && c.Path != "" {
			header += fmt.Sprintf(" %s:%d", c.Path, c.Line)
		}
		_, _ = fmt.Fprintln(w, header)
		writeExternal(w, c.Body)
	}
	return nil
}
