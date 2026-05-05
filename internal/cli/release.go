package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	cmd.AddCommand(newReleasePublishCmd(flags))
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
			if flags.Format == "ndjson" {
				return renderListStreaming(cmd, flags, func(cursor string) ([]any, *provider.Page, error) {
					rels, page, err := p.ListReleases(cmd.Context(), owner, repo, provider.ListReleasesOptions{
						Limit:  flags.Limit,
						Cursor: cursor,
					})
					if err != nil {
						return nil, nil, err
					}
					return toAnySlice(rels), page, nil
				})
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

// publishResult is the JSON-shaped envelope `gaia release publish`
// returns. Agents can branch on `created` (was the release created
// or already existed) and `assets` (which uploaded successfully).
type publishResult struct {
	Tag     string         `json:"tag"`
	Created bool           `json:"created"`
	Release *types.Release `json:"release"`
	Assets  []assetResult  `json:"assets"`
	DryRun  bool           `json:"dry_run,omitempty"`
}

type assetResult struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Uploaded    bool   `json:"uploaded"`
	Error       string `json:"error,omitempty"`
}

func newReleasePublishCmd(flags *globalFlags) *cobra.Command {
	var (
		assets     []string
		notesFrom  string
		prerelease bool
		draft      bool
		dryRun     bool
	)
	cmd := &cobra.Command{
		Use:   "publish <tag>",
		Short: "Publish a release: create-if-missing then upload assets",
		Long: `Orchestrates the full create-or-get-release + asset-upload flow
in one call. Useful in CI workflows: instead of curl-piping JSON
and managing release IDs by hand, the workflow just runs:

  gaia release publish v0.1.0 \
    --asset 'dist/*.tar.gz' \
    --asset 'dist/*.zip' \
    --asset 'dist/*checksums.txt' \
    --notes-from CHANGELOG.md

If a release for the tag already exists, --notes-from is ignored
and assets are added to the existing release. If it doesn't,
the release is created with notes extracted from --notes-from
(falls back to a placeholder if the file or section isn't
found).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tag := args[0]

			// Glob-expand --asset patterns to a flat file list. Empty
			// glob is silently dropped (a CI invocation that accidentally
			// matches nothing fails loudly when the user notices the
			// release has no assets, NOT here at startup — a stricter
			// rule would surprise local --dry-run users).
			files, err := expandAssetGlobs(assets)
			if err != nil {
				return err
			}

			notes := readReleaseNotes(notesFrom, tag)

			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}

			result := publishResult{Tag: tag, DryRun: dryRun}

			if dryRun {
				result.Assets = make([]assetResult, len(files))
				for i, f := range files {
					info, _ := os.Stat(f)
					name := filepath.Base(f)
					ar := assetResult{Name: name, Path: f, ContentType: contentTypeFor(name)}
					if info != nil {
						ar.Size = info.Size()
					}
					result.Assets[i] = ar
				}
				return renderEnvelope(cmd, flags, result, nil, nil)
			}

			// Get-or-create the release.
			rel, err := p.GetRelease(cmd.Context(), owner, repo, tag)
			if err != nil {
				if exitcode.Of(err) != exitcode.NotFound {
					return err
				}
				rel, err = p.CreateRelease(cmd.Context(), owner, repo, provider.CreateReleaseOptions{
					TagName:    tag,
					Name:       tag,
					Body:       notes,
					Draft:      draft,
					Prerelease: prerelease,
				})
				if err != nil {
					return err
				}
				result.Created = true
			}
			result.Release = rel

			// Upload each asset. Per-asset failures are recorded but
			// don't abort the run — operators want to see which ones
			// got through. Final exit code is non-zero if ANY asset
			// failed (so CI flags it).
			anyFailed := false
			for _, f := range files {
				ar := assetResult{
					Name:        filepath.Base(f),
					Path:        f,
					ContentType: contentTypeFor(filepath.Base(f)),
				}
				info, statErr := os.Stat(f)
				if statErr != nil {
					ar.Error = statErr.Error()
					anyFailed = true
					result.Assets = append(result.Assets, ar)
					continue
				}
				ar.Size = info.Size()

				file, openErr := os.Open(f)
				if openErr != nil {
					ar.Error = openErr.Error()
					anyFailed = true
					result.Assets = append(result.Assets, ar)
					continue
				}
				upErr := p.UploadReleaseAsset(cmd.Context(), owner, repo, rel.ID,
					ar.Name, ar.ContentType, ar.Size, file)
				_ = file.Close()
				if upErr != nil {
					ar.Error = upErr.Error()
					anyFailed = true
				} else {
					ar.Uploaded = true
				}
				result.Assets = append(result.Assets, ar)
			}

			if err := renderEnvelope(cmd, flags, result, nil, nil); err != nil {
				return err
			}
			if anyFailed {
				return exitcode.Errorf(exitcode.Generic,
					"one or more assets failed to upload (see asset.error fields)")
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&assets, "asset", nil, "asset glob pattern (repeatable; e.g., 'dist/*.tar.gz')")
	cmd.Flags().StringVar(&notesFrom, "notes-from", "", "path to a file with release notes; if it's a CHANGELOG-style file, the matching ## [VERSION] section is extracted")
	cmd.Flags().BoolVar(&prerelease, "prerelease", false, "mark the release as a prerelease")
	cmd.Flags().BoolVar(&draft, "draft", false, "create the release as a draft")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the resolved plan without contacting the forge")
	return cmd
}

// expandAssetGlobs walks each --asset pattern through filepath.Glob
// and returns the deduplicated, ordered union. Bare paths (no
// metacharacters) pass through as a single-element match.
func expandAssetGlobs(patterns []string) ([]string, error) {
	seen := map[string]struct{}{}
	out := []string{}
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, exitcode.Errorf(exitcode.Usage, "invalid --asset pattern %q: %v", p, err)
		}
		if len(matches) == 0 {
			// Allow the pattern to be a literal path that just doesn't
			// exist; the per-asset open will report it. This lets
			// `--asset README.md` work even when README isn't there
			// (without surprising people who typed a missing file).
			if !strings.ContainsAny(p, "*?[") {
				matches = []string{p}
			}
		}
		for _, m := range matches {
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out, nil
}

// readReleaseNotes reads `--notes-from` and returns either the full
// file contents OR (if the file looks like Keep-a-Changelog format)
// the section matching the tag. Empty path → empty notes.
//
// Tag-to-section matching strips the leading "v" (so "v0.1.0" picks
// the "## [0.1.0]" header) and stops at the next "## [" line.
func readReleaseNotes(path, tag string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Quick "is this a CHANGELOG?" heuristic: presence of `## [`
	// markdown headers. If so, slice out the section; otherwise return
	// the whole file.
	if !strings.Contains(string(raw), "## [") {
		return string(raw)
	}
	want := strings.TrimPrefix(tag, "v")
	header := "## [" + want + "]"
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, header) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## [") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// contentTypeFor guesses an asset's MIME type from its filename.
// The forge ultimately stores whatever we send; this is for tooling
// downstream of the release page (CDN edge caches, package indexers)
// that key on Content-Type. Bias toward correct-and-narrow over
// generic-octet-stream when the extension is unambiguous.
func contentTypeFor(name string) string {
	switch {
	case strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"):
		return "application/gzip"
	case strings.HasSuffix(name, ".zip"):
		return "application/zip"
	case strings.HasSuffix(name, ".txt"), strings.HasSuffix(name, "checksums.txt"):
		return "text/plain"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".sig"), strings.HasSuffix(name, ".asc"):
		return "application/pgp-signature"
	default:
		return "application/octet-stream"
	}
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
	if r.TargetCommitish != "" {
		_, _ = fmt.Fprintf(w, "  Target:    %s\n", r.TargetCommitish)
	}
	_, _ = fmt.Fprintf(w, "  Created:   %s\n", r.CreatedAt.Format("2006-01-02 15:04"))
	if r.PublishedAt != nil {
		_, _ = fmt.Fprintf(w, "  Published: %s\n", r.PublishedAt.Format("2006-01-02 15:04"))
	}
	if r.Body != "" {
		_, _ = fmt.Fprintln(w)
		writeExternal(w, r.Body)
	}
	return nil
}
