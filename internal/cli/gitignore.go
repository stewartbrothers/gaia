package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/gitignore"
)

// gitignoreResult is the JSON envelope payload for `gaia gitignore
// --format json`. The same shape covers both the print path
// (Missing nil-or-omitted) and the --check path (Missing populated
// with whatever entries the project's .gitignore is lacking).
type gitignoreResult struct {
	Entries []string `json:"entries"`
	Missing []string `json:"missing,omitempty"`
}

// newGitignoreCmd registers `gaia gitignore`.
//
// Two modes share one command:
//
//   - Default (no --check): print the recommended block to stdout.
//     Idiomatic use is `gaia gitignore >> .gitignore`. Default output
//     is raw text — wrapping in a JSON envelope would break the
//     append-via-redirection workflow. `--format json` opts into the
//     standard envelope shape.
//
//   - --check: read .gitignore from the current directory (or
//     `--path`) and exit non-zero if any recommended entries are
//     missing. Pretty/json output lists what's missing; --quiet
//     suppresses output (CI gating). This is the path projects pin
//     in CI to keep `.gaia/credentials*` (and, post-Phase 9, the
//     insights paths) from drifting out of .gitignore over time.
//
// The two modes share the embedded source-of-truth via
// internal/gitignore: `gaia gitignore` and the MCP `gaia://gitignore`
// resource read the same bytes, so neither can drift from the other.
func newGitignoreCmd(flags *globalFlags) *cobra.Command {
	var (
		checkMode bool
		quiet     bool
		pathFlag  string
	)
	cmd := &cobra.Command{
		Use:   "gitignore",
		Short: "Print or verify the recommended .gitignore entries for a gaia-using project",
		Long: `Prints the canonical block of .gitignore entries every project
using gaia should adopt — credentials store, plus the insights-DB
glob siblings (Phase 9) so an in-tree override doesn't accidentally
get committed.

Default invocation prints the block to stdout, suitable for
appending into an existing .gitignore:

    gaia gitignore >> .gitignore

The --check mode reads the project's .gitignore (./.gitignore by
default; --path overrides) and reports any missing entries. Exit
code 0 if the .gitignore covers every recommended entry; non-zero
otherwise. Pair --check with --quiet for CI gating.

The block is //go:embed'd from internal/gitignore/recommended.txt at
build time and is byte-identical to the docs/configuration.md
"Recommended .gitignore entries" section.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			explicitFormat := cmd.Flags().Changed("format")
			flags.Format = format

			if checkMode {
				return runGitignoreCheck(cmd, flags, pathFlag, quiet, explicitFormat)
			}
			// Print mode. --quiet on the print path is meaningless;
			// rather than warn (and surprise a CI script) we silently
			// honour the format choice — explicit json or pretty
			// routes through the envelope renderer; the default (no
			// --format) emits the raw block so `gaia gitignore >>
			// .gitignore` keeps working. Mirrors the `gaia learn`
			// precedent.
			if explicitFormat && (format == "json" || format == "pretty") {
				data := gitignoreResult{Entries: gitignore.Entries()}
				return renderEnvelope(cmd, flags, data, nil, prettyGitignore)
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), gitignore.Recommended)
			return err
		},
	}
	cmd.Flags().BoolVar(&checkMode, "check", false, "verify .gitignore covers the recommended entries; exit non-zero if any are missing")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "suppress output (use with --check for CI gating)")
	cmd.Flags().StringVar(&pathFlag, "path", "", "path to a .gitignore file to check (default: ./.gitignore in the current directory)")
	return cmd
}

// runGitignoreCheck reads the project .gitignore and reports any
// missing entries. Exit code is exitcode.Generic (1) when entries
// are missing — Usage (2) is reserved for "you typed it wrong",
// which doesn't apply: the check ran successfully and the project
// failed it. A NotFound on the .gitignore file itself is treated as
// "every entry is missing" rather than an error: a project with no
// .gitignore at all should get the standard "missing N entries"
// signal so the CI script's same exit-code branch handles it.
func runGitignoreCheck(cmd *cobra.Command, flags *globalFlags, pathFlag string, quiet, explicitFormat bool) error {
	target := pathFlag
	if target == "" {
		target = ".gitignore"
	}
	// Resolve relative paths against the current directory; the
	// caller can also pass an absolute path.
	if !filepath.IsAbs(target) {
		cwd, err := os.Getwd()
		if err != nil {
			return exitcode.Errorf(exitcode.Generic, "resolve cwd: %v", err)
		}
		target = filepath.Join(cwd, target)
	}
	raw, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return exitcode.Errorf(exitcode.Generic, "read %s: %v", target, err)
	}
	missing := gitignore.Missing(string(raw))

	if quiet {
		// Quiet path: no output, exit code only. Same exit-code
		// semantics as the verbose --check path so a CI script can
		// pin its branching on $?.
		if len(missing) > 0 {
			return exitcode.Errorf(exitcode.Generic,
				".gitignore missing %d recommended entries (rerun without --quiet to list them)", len(missing))
		}
		return nil
	}

	// Non-quiet path: tell the operator exactly which entries are
	// missing. JSON and pretty paths share the typed payload via
	// renderEnvelope; the raw-text default emits one entry per line
	// so `gaia gitignore --check | xargs -I{} echo "{}" >> .gitignore`
	// is the obvious shell move.
	if explicitFormat && (flags.Format == "json" || flags.Format == "pretty") {
		data := gitignoreResult{
			Entries: gitignore.Entries(),
			Missing: missing,
		}
		// renderEnvelope dispatches to prettyGitignore when format is
		// "pretty"; the JSON path uses the standard envelope shape.
		// Either way the missing slice round-trips into the renderer.
		if err := renderEnvelope(cmd, flags, data, nil, prettyGitignore); err != nil {
			return err
		}
		if len(missing) > 0 {
			return exitcode.Errorf(exitcode.Generic,
				".gitignore missing %d recommended entries", len(missing))
		}
		return nil
	}

	// Raw-text path (default for --check): emit one missing entry
	// per line, prefixed with a short banner so the operator
	// understands what they're looking at. When nothing is missing,
	// emit a one-line confirmation rather than silence — the
	// operator who ran --check explicitly wanted feedback.
	w := cmd.OutOrStdout()
	if len(missing) == 0 {
		_, _ = fmt.Fprintln(w, ".gitignore covers every recommended entry.")
		return nil
	}
	_, _ = fmt.Fprintf(w, ".gitignore missing %d recommended entries:\n", len(missing))
	for _, m := range missing {
		_, _ = fmt.Fprintln(w, m)
	}
	return exitcode.Errorf(exitcode.Generic,
		".gitignore missing %d recommended entries", len(missing))
}

// prettyGitignore renders a gitignoreResult to w. On the print path
// (no Missing) it emits the embedded block verbatim. On the --check
// path (Missing populated) it emits a banner + one entry per line
// matching the raw-text path above so json/pretty/default produce
// the same human-readable shape.
func prettyGitignore(w io.Writer, data any) error {
	r, ok := data.(gitignoreResult)
	if !ok {
		return fmt.Errorf("prettyGitignore: unexpected type %T", data)
	}
	if len(r.Missing) == 0 {
		_, err := fmt.Fprint(w, gitignore.Recommended)
		return err
	}
	if _, err := fmt.Fprintf(w, ".gitignore missing %d recommended entries:\n", len(r.Missing)); err != nil {
		return err
	}
	for _, m := range r.Missing {
		if _, err := fmt.Fprintln(w, m); err != nil {
			return err
		}
	}
	return nil
}
