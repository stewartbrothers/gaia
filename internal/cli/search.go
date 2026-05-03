package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newSearchCmd(flags *globalFlags) *cobra.Command {
	var kind []string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search issues and pull requests",
		Long: `Searches issues and pull requests across the active forge.

By default scope is the current repo (--repo flag, or git-remote
auto-detect). Pass --repo "" or use a configured cross-repo profile
to search every repo your token can see.

--kind narrows results to "issue" or "pull_request" (repeatable;
both included by default). Each result is {kind, number, title, repo}.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}

			// Search uses opts.Repo to scope. Resolve from flags +
			// auto-detect, but tolerate "no repo found" — that's
			// the cross-repo search case.
			repoSlug := flags.Repo
			if repoSlug == "" {
				if owner, name, rerr := resolveRepo(flags); rerr == nil {
					repoSlug = owner + "/" + name
				}
				// If autodetect failed, fall through with empty repoSlug → cross-repo search.
			}

			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					results, page, err := p.Search(cmd.Context(), query, provider.SearchOptions{
						Kinds:  kind,
						Repo:   repoSlug,
						Limit:  flags.Limit,
						Cursor: cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(results), page, nil
				})
			}
			results, page, err := p.Search(cmd.Context(), query, provider.SearchOptions{
				Kinds:  kind,
				Repo:   repoSlug,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, results, page, prettySearch)
		},
	}
	cmd.Flags().StringSliceVar(&kind, "kind", nil, "filter by kind: issue, pull_request (repeatable; default = both)")
	return cmd
}

func prettySearch(w io.Writer, data any) error {
	results, ok := data.([]types.SearchResult)
	if !ok {
		return fmt.Errorf("prettySearch: unexpected type %T", data)
	}
	if len(results) == 0 {
		_, _ = fmt.Fprintln(w, "(no results)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KIND\tREPO\tNUMBER\tTITLE")
	for _, r := range results {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t#%d\t%s\n", r.Kind, r.RepoFull, r.Number, truncate(r.Title, 70))
	}
	return tw.Flush()
}
