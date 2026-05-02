package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

func newReleaseCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "List, view, create, edit, and delete releases",
	}
	cmd.AddCommand(newReleaseListCmd(flags))
	cmd.AddCommand(newReleaseViewCmd(flags))
	cmd.AddCommand(newReleaseCreateCmd(flags))
	cmd.AddCommand(newReleaseEditCmd(flags))
	cmd.AddCommand(newReleaseDeleteCmd(flags))
	return cmd
}

func newReleaseListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List releases (newest first)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			rels, page, err := p.ListReleases(cmd.Context(), owner, repo, provider.ListReleasesOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, rels, page, prettyReleaseList)
		},
	}
}

func newReleaseViewCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "view <tag>",
		Short: "View one release by tag",
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
			rel, err := p.GetRelease(cmd.Context(), owner, repo, args[0])
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, rel, nil, prettyReleaseView)
		},
	}
}

func newReleaseCreateCmd(flags *globalFlags) *cobra.Command {
	var (
		tag, name, body, target string
		draft, prerelease       bool
		dryRun                  bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new release",
		Long: `Creates a new release on the active repo.

  $ gaia release create --tag v1.0.0 --name "First release" \
                        --body "Initial public release"

  $ gaia release create --tag v0.9.0-rc.1 --prerelease --draft

--target accepts a branch name or commit SHA; defaults to the repo's
default branch when empty.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tag == "" {
				return exitcode.Errorf(exitcode.Usage, "--tag is required")
			}
			b, err := readBody(cmd.InOrStdin(), body)
			if err != nil {
				return err
			}
			opts := provider.CreateReleaseOptions{
				TagName:         tag,
				Name:            name,
				Body:            b,
				TargetCommitish: target,
				Draft:           draft,
				Prerelease:      prerelease,
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
				return printDryRun(cmd, fmt.Sprintf("POST /repos/%s/%s/releases", owner, repo), opts)
			}
			rel, err := p.CreateRelease(cmd.Context(), owner, repo, opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, rel, nil, prettyReleaseView)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "tag name (required); creates the tag if it doesn't exist")
	cmd.Flags().StringVar(&name, "name", "", "release name (defaults to tag)")
	cmd.Flags().StringVar(&body, "body", "", "release notes, or \"-\" for stdin")
	cmd.Flags().StringVar(&target, "target", "", "branch or commit; defaults to default branch")
	cmd.Flags().BoolVar(&draft, "draft", false, "mark as draft (not yet published)")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "mark as prerelease")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without posting")
	return cmd
}

func newReleaseEditCmd(flags *globalFlags) *cobra.Command {
	var (
		rename, name, body string
		draftStr           string
		prereleaseStr      string
		dryRun             bool
	)
	cmd := &cobra.Command{
		Use:   "edit <tag>",
		Short: "Edit a release identified by tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := readBody(cmd.InOrStdin(), body)
			if err != nil {
				return err
			}
			opts := provider.EditReleaseOptions{
				TagName: rename,
				Name:    name,
				Body:    b,
			}
			if v, err := parseTriBool(draftStr, "--draft"); err != nil {
				return err
			} else if v != nil {
				opts.Draft = v
			}
			if v, err := parseTriBool(prereleaseStr, "--prerelease"); err != nil {
				return err
			} else if v != nil {
				opts.Prerelease = v
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
				return printDryRun(cmd, fmt.Sprintf("PATCH /repos/%s/%s/releases (by tag=%q)", owner, repo, args[0]), opts)
			}
			rel, err := p.EditRelease(cmd.Context(), owner, repo, args[0], opts)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, rel, nil, prettyReleaseView)
		},
	}
	cmd.Flags().StringVar(&rename, "rename", "", "new tag name")
	cmd.Flags().StringVar(&name, "name", "", "new release name")
	cmd.Flags().StringVar(&body, "body", "", "new release notes, or \"-\" for stdin")
	cmd.Flags().StringVar(&draftStr, "draft", "", "true to mark draft, false to publish (empty = no change)")
	cmd.Flags().StringVar(&prereleaseStr, "prerelease", "", "true to mark prerelease, false to demote (empty = no change)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the request and exit without patching")
	return cmd
}

func newReleaseDeleteCmd(flags *globalFlags) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <tag>",
		Short: "Delete a release by tag (does NOT delete the underlying git tag)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete release %q. Re-run with --confirm to actually remove.\n", args[0])
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
			if err := p.DeleteRelease(cmd.Context(), owner, repo, args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Deleted release %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete (without this, prints what would happen)")
	return cmd
}

// parseTriBool turns "true"/"false"/"" into *bool/*nil. Same pattern
// as gaia pr edit's --draft handling.
func parseTriBool(s, flag string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		v := true
		return &v, nil
	case "false", "0", "no":
		v := false
		return &v, nil
	case "":
		return nil, nil
	default:
		return nil, exitcode.Errorf(exitcode.Usage, "%s must be true|false (or empty); got %q", flag, s)
	}
}

func prettyReleaseList(w io.Writer, data any) error {
	rels, ok := data.([]types.Release)
	if !ok {
		return fmt.Errorf("prettyReleaseList: unexpected type %T", data)
	}
	if len(rels) == 0 {
		_, _ = fmt.Fprintln(w, "(no releases)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TAG\tNAME\tDRAFT\tPRE\tAUTHOR\tCREATED")
	for _, r := range rels {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%s\t%s\n",
			r.TagName, truncate(r.Name, 40), r.Draft, r.Prerelease,
			r.Author.Login, r.CreatedAt.Format("2006-01-02"))
	}
	return tw.Flush()
}

func prettyReleaseView(w io.Writer, data any) error {
	r, ok := data.(*types.Release)
	if !ok {
		return fmt.Errorf("prettyReleaseView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "%s — %s\n", r.TagName, r.Name)
	_, _ = fmt.Fprintf(w, "  Author:    %s\n", r.Author.Login)
	_, _ = fmt.Fprintf(w, "  Draft:     %v\n", r.Draft)
	_, _ = fmt.Fprintf(w, "  Pre:       %v\n", r.Prerelease)
	_, _ = fmt.Fprintf(w, "  Target:    %s\n", r.TargetCommitish)
	_, _ = fmt.Fprintf(w, "  Created:   %s\n", r.CreatedAt.Format("2006-01-02 15:04"))
	if r.PublishedAt != nil {
		_, _ = fmt.Fprintf(w, "  Published: %s\n", r.PublishedAt.Format("2006-01-02 15:04"))
	}
	if r.Body != "" {
		_, _ = fmt.Fprintf(w, "\n%s\n", r.Body)
	}
	return nil
}
