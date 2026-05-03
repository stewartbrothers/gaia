package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// newWikiCmd builds the `gaia wiki ...` command tree:
//
//	gaia wiki list
//	gaia wiki view   <path>
//	gaia wiki search <query>
//	gaia wiki edit   <path> --body <text|->
//	gaia wiki delete <path> [--confirm]
//
// All five operations follow the trimmed-output / envelope conventions
// used elsewhere — JSON by default, --format pretty for human output.
// The search subcommand caps its scan at a documented page count and
// notes the limit in its help text so agents can plan around it.
func newWikiCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiki",
		Short: "List, view, search, edit, and delete wiki pages",
	}
	cmd.AddCommand(newWikiListCmd(flags))
	cmd.AddCommand(newWikiViewCmd(flags))
	cmd.AddCommand(newWikiSearchCmd(flags))
	cmd.AddCommand(newWikiEditCmd(flags))
	cmd.AddCommand(newWikiDeleteCmd(flags))
	return cmd
}

func newWikiListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List wiki pages (title + path; bodies fetched per-page via view)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					pages, page, err := p.ListWikiPages(cmd.Context(), owner, repo, provider.ListWikiPagesOptions{
						Limit:  flags.Limit,
						Cursor: cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(pages), page, nil
				})
			}
			pages, page, err := p.ListWikiPages(cmd.Context(), owner, repo, provider.ListWikiPagesOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pages, page, prettyWikiList)
		},
	}
}

func newWikiViewCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "view <path>",
		Short: "View one wiki page (title + body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			page, err := p.GetWikiPage(cmd.Context(), owner, repo, args[0])
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, page, nil, prettyWikiView)
		},
	}
}

func newWikiSearchCmd(flags *globalFlags) *cobra.Command {
	var maxPages int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search wiki pages (client-side; capped at --max-pages)",
		Long: `Searches every wiki page on the repo for the query and returns
title + snippet hits. Forgejo has no native wiki-search endpoint, so
the search runs client-side: gaia lists pages then fetches each one's
body to scan it. The scan is capped at --max-pages (default 100).
Larger wikis should narrow the query rather than raise the cap.

The agent-cost win is that one ` + "`gaia wiki search`" + ` call
replaces N WebFetch calls — see docs/dogfood-comparison.md.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			hits, err := p.SearchWikiPages(cmd.Context(), owner, repo, args[0], provider.SearchWikiOptions{
				MaxPages: maxPages,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, hits, nil, prettyWikiSearch)
		},
	}
	cmd.Flags().IntVar(&maxPages, "max-pages", 0, "cap on pages scanned (0 = default 100)")
	return cmd
}

func newWikiEditCmd(flags *globalFlags) *cobra.Command {
	var (
		body   string
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "edit <path>",
		Short: "Create or replace a wiki page (--body - reads stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := readBody(cmd.InOrStdin(), body)
			if err != nil {
				return err
			}
			if b == "" {
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
			if dryRun {
				return printDryRun(cmd, fmt.Sprintf("PUT (upsert) /repos/%s/%s/wiki/page/%s", owner, repo, args[0]),
					map[string]any{"slug": args[0], "body_bytes": len(b)})
			}
			page, err := p.EditWikiPage(cmd.Context(), owner, repo, args[0], b)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, page, nil, prettyWikiView)
		},
	}
	cmd.Flags().StringVar(&body, "body", "", "page body, or \"-\" for stdin")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without writing")
	return cmd
}

func newWikiDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <path>",
		Short: "Delete a wiki page (requires --confirm; preview otherwise)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete wiki page %q. Re-run with --confirm to actually remove.\n", args[0])
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
			if err := p.DeleteWikiPage(cmd.Context(), owner, repo, args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted wiki page %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete (without this, prints what would happen)")
	return cmd
}

func prettyWikiList(w io.Writer, data any) error {
	pages, ok := data.([]types.WikiPage)
	if !ok {
		return fmt.Errorf("prettyWikiList: unexpected type %T", data)
	}
	if len(pages) == 0 {
		_, _ = fmt.Fprintln(w, "(no wiki pages)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PATH\tTITLE\tLAST_COMMIT\tUPDATED")
	for _, p := range pages {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			p.Path, truncate(p.Title, 50), p.LastCommit, p.UpdatedAt.Format("2006-01-02"))
	}
	return tw.Flush()
}

func prettyWikiView(w io.Writer, data any) error {
	page, ok := data.(*types.WikiPage)
	if !ok {
		return fmt.Errorf("prettyWikiView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "%s (%s)\n", page.Title, page.Path)
	if page.LastCommit != "" {
		_, _ = fmt.Fprintf(w, "  Last commit: %s\n", page.LastCommit)
	}
	if !page.UpdatedAt.IsZero() {
		_, _ = fmt.Fprintf(w, "  Updated:     %s\n", page.UpdatedAt.Format("2006-01-02 15:04"))
	}
	if page.Body != "" {
		_, _ = fmt.Fprintln(w)
		writeExternal(w, page.Body)
	}
	return nil
}

func prettyWikiSearch(w io.Writer, data any) error {
	hits, ok := data.([]types.WikiSearchHit)
	if !ok {
		return fmt.Errorf("prettyWikiSearch: unexpected type %T", data)
	}
	if len(hits) == 0 {
		_, _ = fmt.Fprintln(w, "(no matches)")
		return nil
	}
	for _, h := range hits {
		_, _ = fmt.Fprintf(w, "%s — %s\n", h.Path, h.Title)
		if h.Snippet != "" {
			_, _ = fmt.Fprintf(w, "  %s\n", h.Snippet)
		}
	}
	return nil
}
