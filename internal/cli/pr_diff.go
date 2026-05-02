package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newPRDiffCmd(flags *globalFlags) *cobra.Command {
	var paths []string

	cmd := &cobra.Command{
		Use:   "diff <number>",
		Short: "Print the structured diff for a PR",
		Long: `Fetches the PR's unified diff and parses it into a
{path, status, hunks: [{header, old_start, old_lines, new_start,
new_lines, lines}]} array. JSON output is the structured form;
pretty output reconstructs the unified-diff representation that
git shows.

--paths narrows the result to a subset of files; renamed files
match by either current or pre-rename path.`,
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
			files, err := p.GetPullRequestDiff(cmd.Context(), owner, repo, n, provider.GetPullRequestDiffOptions{
				Paths: paths,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, files, nil, prettyPRDiff)
		},
	}
	cmd.Flags().StringSliceVar(&paths, "paths", nil, "filter to specific file paths (repeatable)")
	return cmd
}

func prettyPRDiff(w io.Writer, data any) error {
	files, ok := data.([]types.DiffFile)
	if !ok {
		return fmt.Errorf("prettyPRDiff: unexpected type %T", data)
	}
	for _, f := range files {
		header := f.Path
		if f.OldPath != "" && f.OldPath != f.Path {
			header = f.OldPath + " → " + f.Path
		}
		_, _ = fmt.Fprintf(w, "=== %s [%s] ===\n", header, f.Status)
		if f.Binary {
			_, _ = fmt.Fprintln(w, "(binary)")
			continue
		}
		for _, h := range f.Hunks {
			_, _ = fmt.Fprintln(w, h.Header)
			for _, line := range h.Lines {
				_, _ = fmt.Fprintln(w, line)
			}
		}
		_, _ = fmt.Fprintln(w)
	}
	return nil
}
