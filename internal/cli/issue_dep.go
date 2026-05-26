package cli

import (
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// newIssueDepCmd is the parent for `gaia issue dep` — list / add /
// remove issue-dependency relationships. Two directions exist
// (blockers = issues blocking this one; blocks = issues this one is
// blocking), but they describe the same edge — POST/DELETE on the
// dependency endpoint covers both via the inverse framing. See #317.
func newIssueDepCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dep",
		Short: "List, add, and remove issue dependencies (blockers + blocks)",
		Long: `Manage issue-dependency relationships.

Two directions exist:

  blockers — issues blocking this one (this issue depends on them)
  blocks   — issues this one is blocking (the inverse view)

"X blocks Y" and "Y depends on X" describe the same edge from
different framings. add/remove accept either --blocker or --blocks
and map both to the same underlying op.

Works on both Forgejo and GitHub (GitHub REST landed in API
version 2026-03-10; the wire shapes differ but the gaia surface
is uniform — see docs/provider-parity.md).`,
	}
	cmd.AddCommand(newIssueDepListCmd(flags))
	cmd.AddCommand(newIssueDepAddCmd(flags))
	cmd.AddCommand(newIssueDepRemoveCmd(flags))
	return cmd
}

func newIssueDepListCmd(flags *globalFlags) *cobra.Command {
	var direction string

	cmd := &cobra.Command{
		Use:   "list <number>",
		Short: "List the dependencies (blockers) or blocks of an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			if direction != "blockers" && direction != "blocks" {
				return exitcode.Errorf(exitcode.Usage,
					`--direction must be "blockers" or "blocks", got %q`, direction)
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			owner, repo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			po := provider.ListIssueDepsOptions{
				Limit:  flags.Limit,
				Cursor: flags.Cursor,
			}
			var issues []types.Issue
			var page *provider.Page
			switch direction {
			case "blockers":
				issues, page, err = p.ListIssueDependencies(cmd.Context(), owner, repo, n, po)
			case "blocks":
				issues, page, err = p.ListIssueBlocks(cmd.Context(), owner, repo, n, po)
			}
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, issues, page, prettyIssueList)
		},
	}
	cmd.Flags().StringVar(&direction, "direction", "blockers",
		`which side of the edge to list: "blockers" (default) or "blocks"`)
	return cmd
}

// newIssueDepAddCmd makes a dependency edge. The CLI accepts both
// framings via mutually-exclusive flags, and either same-repo or
// cross-repo (#325) refs:
//
//	--blocker 7              → "7 blocks N" same-repo
//	--blocker owner/repo#7   → "owner/repo#7 blocks N" cross-repo
//	--blocks  M              → "N blocks M" same-repo (inverse framing)
//	--blocks  owner/repo#M   → "N blocks owner/repo#M" cross-repo
//
// Same edge from different framings; the inverse is just the
// host/target swap. The forge provider echoes the added blocker back
// as the response; we render it as a single-issue envelope.
func newIssueDepAddCmd(flags *globalFlags) *cobra.Command {
	var blocker, blocks string

	cmd := &cobra.Command{
		Use:   "add <number>",
		Short: "Add a dependency edge — either --blocker M (M blocks N) or --blocks M (N blocks M); M may be `owner/repo#N` for cross-repo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			hostOwner, hostRepo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			host, target, err := resolveDepDirection(n, hostOwner, hostRepo, blocker, blocks)
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			added, err := p.AddIssueDependency(cmd.Context(), host.Owner, host.Repo, host.Number, target)
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, added, nil, prettyIssueView)
		},
	}
	cmd.Flags().StringVar(&blocker, "blocker", "",
		"issue that blocks the argument issue (bare number for same-repo, owner/repo#N for cross-repo)")
	cmd.Flags().StringVar(&blocks, "blocks", "",
		"issue that the argument issue blocks (bare number for same-repo, owner/repo#N for cross-repo)")
	return cmd
}

func newIssueDepRemoveCmd(flags *globalFlags) *cobra.Command {
	var blocker, blocks string

	cmd := &cobra.Command{
		Use:   "remove <number>",
		Short: "Remove a dependency edge — same --blocker/--blocks shape as add (incl. owner/repo#N cross-repo refs)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := parseIssueNumber(args[0])
			if err != nil {
				return err
			}
			hostOwner, hostRepo, err := resolveRepo(flags)
			if err != nil {
				return err
			}
			host, target, err := resolveDepDirection(n, hostOwner, hostRepo, blocker, blocks)
			if err != nil {
				return err
			}
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			if err := p.RemoveIssueDependency(cmd.Context(), host.Owner, host.Repo, host.Number, target); err != nil {
				return err
			}
			// Mirror the milestone-delete shape: no body, just an empty
			// envelope so callers see a successful exit.
			return renderEnvelope(cmd, flags, struct{}{}, nil, prettyIssueDepRemoveOK)
		},
	}
	cmd.Flags().StringVar(&blocker, "blocker", "",
		"issue that blocks the argument issue (bare number for same-repo, owner/repo#N for cross-repo)")
	cmd.Flags().StringVar(&blocks, "blocks", "",
		"issue that the argument issue blocks (bare number for same-repo, owner/repo#N for cross-repo)")
	return cmd
}

// depAnchor is the parsed location of the host or target side of a
// dependency edge. Same-repo refs inherit the host repo from the
// active gaia config; cross-repo refs carry their own owner+repo.
type depAnchor struct {
	Owner  string
	Repo   string
	Number int
}

// crossRepoRefPattern matches the cross-repo CLI reference shape
// `owner/repo#N` — the GitHub-flavored convention. Anchored so a
// bare integer doesn't accidentally match.
var crossRepoRefPattern = regexp.MustCompile(`^([^/\s#]+)/([^/\s#]+)#(\d+)$`)

// parseDepRefString parses a CLI flag value like "7" (same-repo) or
// "owner/repo#7" (cross-repo) into a (owner, repo, number) triple.
// Empty owner/repo means the caller should fall back to the host's
// repo. Returns a Usage exit-coded error for malformed input.
func parseDepRefString(s string) (anchor depAnchor, err error) {
	if m := crossRepoRefPattern.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[3]) // regex guarantees digits
		if n <= 0 {
			return depAnchor{}, exitcode.Errorf(exitcode.Usage,
				"dep reference %q has non-positive issue number", s)
		}
		return depAnchor{Owner: m[1], Repo: m[2], Number: n}, nil
	}
	// Bare integer = same-repo.
	n, perr := strconv.Atoi(s)
	if perr != nil || n <= 0 {
		return depAnchor{}, exitcode.Errorf(exitcode.Usage,
			"dep reference %q must be a positive integer or owner/repo#N", s)
	}
	return depAnchor{Number: n}, nil
}

// resolveDepDirection enforces the mutual exclusion of --blocker /
// --blocks and returns (host, target) anchors such that calling
// AddIssueDependency(host.Owner, host.Repo, host.Number, IssueDepRef{
// target.Owner, target.Repo, target.Number}) creates the edge.
//
// Naming reads:
//
//   - --blocker M on issue N → "M blocks N." Edge stored on N's
//     /dependencies. Host=N, Target=M.
//   - --blocks M on issue N → "N blocks M." Same edge, framed from
//     the other side. Edge stored on M's /dependencies. Host=M,
//     Target=N.
//
// Either side may be cross-repo — the host repo follows the side
// that owns the edge, so e.g. --blocks owner/repo#M means the edge
// lives on owner/repo's /dependencies (host = M in owner/repo).
//
// Exactly one of the two flags must be populated.
func resolveDepDirection(n int, hostOwner, hostRepo, blocker, blocks string) (host depAnchor, target provider.IssueDepRef, err error) {
	switch {
	case blocker != "" && blocks != "":
		return depAnchor{}, provider.IssueDepRef{}, exitcode.Errorf(exitcode.Usage,
			"--blocker and --blocks are mutually exclusive")
	case blocker != "":
		// "M blocks N" — host=N (the argument), target=M (the flag).
		other, err := parseDepRefString(blocker)
		if err != nil {
			return depAnchor{}, provider.IssueDepRef{}, err
		}
		host = depAnchor{Owner: hostOwner, Repo: hostRepo, Number: n}
		target = provider.IssueDepRef{Owner: other.Owner, Repo: other.Repo, Number: other.Number}
		return host, target, nil
	case blocks != "":
		// "N blocks M" — host=M (the flag), target=N (the argument).
		other, err := parseDepRefString(blocks)
		if err != nil {
			return depAnchor{}, provider.IssueDepRef{}, err
		}
		hostOwnerFinal, hostRepoFinal := hostOwner, hostRepo
		if !other.sameRepo() {
			hostOwnerFinal, hostRepoFinal = other.Owner, other.Repo
		}
		host = depAnchor{Owner: hostOwnerFinal, Repo: hostRepoFinal, Number: other.Number}
		target = provider.IssueDepRef{Number: n} // target = the argument issue, same as host repo
		// But if --blocks pointed at a different repo, the target
		// (which is the CLI argument issue) lives in OUR repo, not
		// the flag's repo. Surface that via owner/repo on the ref.
		if !other.sameRepo() {
			target.Owner = hostOwner
			target.Repo = hostRepo
		}
		return host, target, nil
	default:
		return depAnchor{}, provider.IssueDepRef{}, exitcode.Errorf(exitcode.Usage,
			"one of --blocker or --blocks is required")
	}
}

// sameRepo reports whether the anchor has no owner/repo (and so
// should inherit the host's).
func (a depAnchor) sameRepo() bool {
	return a.Owner == "" && a.Repo == ""
}

func prettyIssueDepRemoveOK(w io.Writer, _ any) error {
	_, err := fmt.Fprintln(w, "dependency edge removed")
	return err
}
