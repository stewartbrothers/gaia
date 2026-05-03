// Package cli implements the gaia command-line interface. It is
// internal because the CLI surface is for end-users and agents, not
// downstream Go consumers — the Go-level API is core/* and is the
// stable contract.
//
// Each subcommand is a thin wrapper that:
//
//  1. Loads layered config + env + flag overrides via core/config.
//  2. Builds the appropriate Provider from core/forgejo (Phase 1).
//  3. Calls one Provider method.
//  4. Wraps the result in core/envelope and renders to stdout.
//
// Errors propagate as exitcode.Error values; the main wrapper in
// cmd/gaia/main.go translates the error into the process exit code.
package cli

import (
	"github.com/spf13/cobra"
)

// globalFlags carries every persistent flag exposed on the root
// command. Each subcommand reads from this struct rather than hitting
// os.Getenv or duplicating cobra wiring.
type globalFlags struct {
	Profile           string
	Provider          string
	APIURL            string
	Repo              string
	Format            string
	Fields            string
	Limit             int
	Cursor            string
	NoExternalMarkers bool
	// NoCache, when true, bypasses the local read cache for this
	// invocation only. Useful for an agent that needs an authoritative
	// read after a known mutation it didn't drive itself (e.g. a forge
	// admin closed an issue out-of-band). Persistent flag — applies
	// to every subcommand on the call. (#42)
	NoCache bool
}

// NewRootCmd constructs a fresh root command tree. Each call returns
// an independent *cobra.Command so tests don't share state across
// invocations.
func NewRootCmd() *cobra.Command {
	flags := &globalFlags{}

	root := &cobra.Command{
		Use:   "gaia",
		Short: "Token-trimmed CLI for Forgejo and GitHub",
		Long: `gaia is a Git AI Access tool: a CLI and MCP server providing
agent-shaped, token-trimmed responses against Forgejo and GitHub.
Output goes to stdout in JSON by default; use --format=pretty for a
human-readable rendering.

See docs/output-format.md for the response envelope, docs/exit-codes.md
for the exit-code convention, and docs/configuration.md for config and
auth setup.`,
		// Cobra prints usage on every error by default; we don't want
		// that — error output should be a single line, not a wall of
		// help text. Errors are rendered by main() via exitcode.Of.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flags.Profile, "profile", "", "config profile name")
	pf.StringVar(&flags.Provider, "provider", "", "provider: forgejo or github")
	pf.StringVar(&flags.APIURL, "api-url", "", "API base URL override")
	pf.StringVar(&flags.Repo, "repo", "", "owner/name (overrides git-remote autodetect)")
	pf.StringVarP(&flags.Format, "format", "F", "json", "output format: json, pretty, or ndjson (ndjson streams list commands one item per line; rejected on single-resource commands)")
	pf.StringVar(&flags.Fields, "fields", "", "field projection, e.g. number,title,labels.name")
	pf.IntVar(&flags.Limit, "limit", 0, "page limit (0 = default 30)")
	pf.StringVar(&flags.Cursor, "cursor", "", "pagination cursor from previous response")
	pf.BoolVar(&flags.NoExternalMarkers, "no-external-markers", false, "in --format pretty, render forge-supplied user content (issue/PR/wiki bodies, comments, etc.) verbatim instead of wrapping in <<<EXTERNAL / EXTERNAL>>> delimiters; JSON output is unaffected (#146)")
	pf.BoolVar(&flags.NoCache, "no-cache", false, "bypass the local read cache for this call — every read goes upstream (#42)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newWhoamiCmd(flags))
	root.AddCommand(newAuthCmd(flags))
	root.AddCommand(newIssueCmd(flags))
	root.AddCommand(newPRCmd(flags))
	root.AddCommand(newSearchCmd(flags))
	root.AddCommand(newLabelCmd(flags))
	root.AddCommand(newReleaseCmd(flags))
	root.AddCommand(newChainCmd(flags))
	root.AddCommand(newPackagesCmd(flags))
	root.AddCommand(newWikiCmd(flags))
	root.AddCommand(newWebhookCmd(flags))
	root.AddCommand(newCacheCmd(flags))

	return root
}

// Execute is the convenience entry point used by cmd/gaia/main.go.
func Execute() error {
	return NewRootCmd().Execute()
}
