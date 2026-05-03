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

// packageFlags carries the per-subcommand flags shared between
// list, view, delete. --owner is the package-owner override; falls
// back to the repo owner from --repo / project config / git remote
// the same way other commands do (since packages live under a user
// or org, not a repo).
type packageFlags struct {
	owner string
}

func newPackagesCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "packages",
		Short: "List, view, and delete packages (registry artifacts)",
		Long: `Inspect packages on a Forgejo package registry. Packages live
under a user or org (not a repo), so commands take --owner rather
than --repo. When --owner is omitted, the repo owner from --repo
/ .gaia/config.yaml / git-remote autodetect is used.

Package specs use the form <type>/<name>/<version>:

  $ gaia packages list  --owner Gerwood --type generic
  $ gaia packages view  --owner Gerwood generic/myapp/1.2.0
  $ gaia packages delete --owner Gerwood generic/myapp/1.2.0 --confirm

Upload (PUT to a generic-package endpoint) is intentionally NOT
in this command — it's tracked in follow-up issue #122.`,
	}
	cmd.AddCommand(newPackagesListCmd(flags))
	cmd.AddCommand(newPackagesViewCmd(flags))
	cmd.AddCommand(newPackagesDeleteCmd(flags))
	return cmd
}

// resolvePackageOwner returns the owner to use. Priority:
//  1. --owner flag (explicit).
//  2. resolveRepo() owner — i.e. the same path every other repo-scoped
//     command uses, so an agent inside a configured checkout can omit
//     the flag entirely.
func resolvePackageOwner(g *globalFlags, p *packageFlags) (string, error) {
	if p.owner != "" {
		return p.owner, nil
	}
	owner, _, err := resolveRepo(g)
	if err != nil {
		return "", exitcode.Errorf(exitcode.Usage,
			"--owner is required (could not auto-detect: %v)", err)
	}
	return owner, nil
}

func newPackagesListCmd(flags *globalFlags) *cobra.Command {
	pf := &packageFlags{}
	var pkgType, q string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List packages owned by --owner",
		RunE: func(cmd *cobra.Command, _ []string) error {
			owner, err := resolvePackageOwner(flags, pf)
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			pkgs, page, err := p.ListPackages(cmd.Context(), owner, provider.ListPackagesOptions{
				Type:   pkgType,
				Q:      q,
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			})
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pkgs, page, prettyPackagesList)
		},
	}
	cmd.Flags().StringVar(&pf.owner, "owner", "", "package owner (user/org); defaults to the repo owner")
	cmd.Flags().StringVar(&pkgType, "type", "", "filter by registry kind (npm|maven|container|generic|...)")
	cmd.Flags().StringVar(&q, "q", "", "name-substring filter")
	return cmd
}

func newPackagesViewCmd(flags *globalFlags) *cobra.Command {
	pf := &packageFlags{}
	cmd := &cobra.Command{
		Use:   "view <type>/<name>/<version>",
		Short: "View one package version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgType, name, version, err := splitPackageSpec(args[0])
			if err != nil {
				return err
			}
			owner, err := resolvePackageOwner(flags, pf)
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			pkg, err := p.GetPackage(cmd.Context(), owner, pkgType, name, version)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, pkg, nil, prettyPackageView)
		},
	}
	cmd.Flags().StringVar(&pf.owner, "owner", "", "package owner (user/org); defaults to the repo owner")
	return cmd
}

func newPackagesDeleteCmd(flags *globalFlags) *cobra.Command {
	pf := &packageFlags{}
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete <type>/<name>/<version>",
		Short: "Delete one package version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgType, name, version, err := splitPackageSpec(args[0])
			if err != nil {
				return err
			}
			if !confirm {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"Would delete package %s/%s/%s. Re-run with --confirm to actually remove.\n",
					pkgType, name, version)
				return nil
			}
			owner, err := resolvePackageOwner(flags, pf)
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			if err := p.DeletePackage(cmd.Context(), owner, pkgType, name, version); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"✓ Deleted package %s/%s/%s\n", pkgType, name, version)
			return nil
		},
	}
	cmd.Flags().StringVar(&pf.owner, "owner", "", "package owner (user/org); defaults to the repo owner")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually delete (without this, prints what would happen)")
	return cmd
}

// splitPackageSpec parses "<type>/<name>/<version>" into its three
// parts. Names that themselves contain "/" (npm scoped names like
// @scope/pkg) need exactly two slash-separators on the OUTER path —
// i.e. the user passes "npm/@scope%2Fpkg/1.0.0" with the inner
// slash pre-escaped, OR the agent invokes via MCP where the args
// arrive as separate fields.
//
// We err-on-the-side-of-strict and only accept exactly three
// segments so a malformed spec fails loud at parse time rather
// than 404'ing the upstream.
func splitPackageSpec(spec string) (pkgType, name, version string, err error) {
	parts := strings.SplitN(spec, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", exitcode.Errorf(exitcode.Usage,
			"package spec %q must be <type>/<name>/<version>", spec)
	}
	return parts[0], parts[1], parts[2], nil
}

func prettyPackagesList(w io.Writer, data any) error {
	pkgs, ok := data.([]types.Package)
	if !ok {
		return fmt.Errorf("prettyPackagesList: unexpected type %T", data)
	}
	if len(pkgs) == 0 {
		_, _ = fmt.Fprintln(w, "(no packages)")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TYPE\tNAME\tVERSION\tOWNER\tCREATED")
	for _, p := range pkgs {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			p.Type, p.Name, p.Version, p.Owner,
			p.CreatedAt.Format("2006-01-02"))
	}
	return tw.Flush()
}

func prettyPackageView(w io.Writer, data any) error {
	p, ok := data.(*types.Package)
	if !ok {
		return fmt.Errorf("prettyPackageView: unexpected type %T", data)
	}
	_, _ = fmt.Fprintf(w, "%s/%s/%s\n", p.Type, p.Name, p.Version)
	_, _ = fmt.Fprintf(w, "  Owner:   %s\n", p.Owner)
	_, _ = fmt.Fprintf(w, "  Created: %s\n", p.CreatedAt.Format("2006-01-02 15:04"))
	if p.Size > 0 {
		_, _ = fmt.Fprintf(w, "  Size:    %d\n", p.Size)
	}
	return nil
}
