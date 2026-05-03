package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/chain"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

// staleAge is how long a yielded chain's state file lingers before
// `gaia chain` opportunistically cleans it up. 24h covers "I yielded
// yesterday and want to resume today" without piling up cruft from
// abandoned chains.
const staleAge = 24 * time.Hour

func newChainCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chain",
		Short: "Run a chain of steps with success / failure / yield routing",
		Long: `Chains let you describe a multi-step workflow once and have
gaia run it in one CLI invocation, returning a single envelope
with success / failure / yield / abort routing.

Subcommands:

  run     — start a chain from a YAML file
  resume  — pick up a chain that yielded earlier (state on disk)
  list    — show chains that yielded but haven't been resumed
  abort   — discard a yielded chain without resuming

State for yielded chains lives at $XDG_STATE_HOME/gaia/chains/
(falls back to ~/.local/state/gaia/chains/). Files are cleaned
up automatically after 24h of inactivity.

See docs/chain.md for the YAML schema and worked examples.`,
	}
	cmd.AddCommand(newChainRunCmd(flags))
	cmd.AddCommand(newChainResumeCmd(flags))
	cmd.AddCommand(newChainListCmd(flags))
	cmd.AddCommand(newChainAbortCmd(flags))
	return cmd
}

// resolveStateDir returns the chain state directory, opportunistically
// cleaning out files older than staleAge. Errors during cleanup are
// best-effort silent — operators see them via `gaia chain list` if
// they really need to inspect the directory.
func resolveStateDir() (string, error) {
	dir, err := chain.DefaultStateDir()
	if err != nil {
		return "", exitcode.Wrap(err, exitcode.Generic, "chain state directory")
	}
	_, _ = chain.CleanupStale(dir, staleAge)
	return dir, nil
}

func newChainRunCmd(flags *globalFlags) *cobra.Command {
	var (
		chainFile string
		varFlags  []string
		dryRun    bool
		verbose   bool
	)
	cmd := &cobra.Command{
		Use:   "run [<name-or-path>]",
		Short: "Run a chain by saved name or YAML file path",
		Long: `Run a chain definition.

  gaia chain run pr-create-and-land \
    --var title="feat: thing" \
    --var body="description" \
    --var head=feature/x

  gaia chain run --chain-file ./ci.yaml      # ad-hoc, file by path
  gaia chain run ./ci.yaml                   # equivalent positional

The positional argument resolves in this order:

  1. literal path (separator or .yaml/.yml suffix) → use as-is
  2. project saved chain → .gaia/chains/<name>.yaml
  3. global saved chain  → ~/.config/gaia/chains/<name>.yaml
  4. none found → usage error listing the attempted paths

--chain-file is the explicit, scriptable form: it always takes a
path and bypasses the saved-chain lookup. When both --chain-file
and a positional argument are supplied, --chain-file wins.

--var is repeatable; values containing '=' split on the first one
only ('--var msg=a=b' → key=msg, value=a=b).

Exit codes:
  0  chain succeeded (or yielded — agent reads the envelope)
  1  chain failed (Result.Failure has details) or aborted
  2  usage error (no chain specified, missing file, var validation)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveChainArg(chainFile, args)
			if err != nil {
				return err
			}
			c, err := chain.ParseFile(resolved)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Usage, "load chain")
			}

			vars, err := parseVarFlags(varFlags)
			if err != nil {
				return err
			}

			stateDir, err := resolveStateDir()
			if err != nil {
				return err
			}
			opts := chain.RunOptions{
				DryRun:           dryRun,
				StateDir:         stateDir,
				SubChainResolver: subChainResolver(),
			}
			if verbose {
				opts.Progress = cmd.ErrOrStderr()
			}

			res, err := chain.Run(cmd.Context(), c, vars, opts)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Usage, "run chain")
			}

			if err := renderEnvelope(cmd, flags, res, nil, nil); err != nil {
				return err
			}

			return chainExitFromStatus(res)
		},
	}
	cmd.Flags().StringVar(&chainFile, "chain-file", "", "path to a chain YAML definition (overrides positional name)")
	cmd.Flags().StringSliceVar(&varFlags, "var", nil, "chain input as key=value (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "substitute vars + render the resolved plan, but don't execute")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream per-step progress to stderr while the chain runs")
	return cmd
}

// resolveChainArg resolves the chain argument supplied on the
// command line. --chain-file (explicit) takes precedence; otherwise
// the positional argument is run through chain.Resolve against the
// current project root + global chain dir. Returns the resolved
// filesystem path (suitable for chain.ParseFile).
func resolveChainArg(chainFile string, args []string) (string, error) {
	if chainFile != "" {
		return chainFile, nil
	}
	if len(args) == 0 {
		return "", exitcode.Errorf(exitcode.Usage,
			"chain name or --chain-file is required (try `gaia chain run pr-create-and-land` or `gaia chain run --chain-file ./ci.yaml`)")
	}
	resolved, err := chain.Resolve(args[0], chainResolveOptions())
	if err != nil {
		// Surface the attempts list directly — the operator wants to
		// see exactly which paths were tried.
		return "", exitcode.Wrap(err, exitcode.Usage, "resolve chain")
	}
	return resolved, nil
}

// subChainResolver wraps chainResolveOptions + chain.Resolve +
// chain.ParseFile into a single closure suitable for
// chain.RunOptions.SubChainResolver. Phase C / #149 chain
// composition uses this on every nested-chain step so a Forgejo-
// resident chain can call another saved chain ("open-and-land")
// without manual orchestration.
//
// Errors propagate the resolution path the operator can read —
// "chain X: not found (tried: <a>, <b>)" — rather than collapsing
// to a generic "missing chain". Parse errors include the resolved
// file path so an operator running `vim` against the right file
// is one click away.
func subChainResolver() func(string) (*chain.Chain, error) {
	opts := chainResolveOptions()
	return func(name string) (*chain.Chain, error) {
		path, err := chain.Resolve(name, opts)
		if err != nil {
			return nil, err
		}
		return chain.ParseFile(path)
	}
}

// chainResolveOptions builds the project + global lookup directives
// `chain.Resolve` consumes. ProjectRoot uses the same auth.ProjectRoot
// helper credentials use; GlobalDir is `~/.config/gaia/chains/`.
//
// Either layer is silently skipped when the helper fails (no git
// project / no resolvable home) — Resolve handles "" meaning
// "skip this layer".
func chainResolveOptions() chain.ResolveOptions {
	opts := chain.ResolveOptions{}
	if cwd, err := os.Getwd(); err == nil {
		if root := auth.ProjectRoot(cwd); root != "" {
			opts.ProjectRoot = root
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		opts.GlobalDir = filepath.Join(home, ".config", "gaia", "chains")
	}
	return opts
}

func newChainResumeCmd(flags *globalFlags) *cobra.Command {
	var (
		decision   string
		modifyStep string
		modifyVars []string
	)
	cmd := &cobra.Command{
		Use:   "resume <token>",
		Short: "Resume a chain that yielded earlier",
		Long: `Pick up a chain that paused on a yield_on condition.

  gaia chain resume <token> [--decision continue|abort|modify [--modify-step ID --modify-vars k=v,...]]

The token is the resume_token from the original ` + "`gaia chain run`" + `
envelope. Use ` + "`gaia chain list`" + ` to see currently-yielded chains
if you've lost the token.

--decision options:
  continue   re-run the yielded step (default). Useful after
             you've fixed the underlying cause (e.g., pushed a
             commit, retried a transient outage).
  abort      discard the yielded chain. Equivalent to
             ` + "`gaia chain abort`" + `.
  modify     change the yielded step's vars before re-running.
             Requires --modify-step <step-id> matching the
             yielded step + one or more --modify-vars k=v
             entries (repeatable; comma-separated also accepted).

Modify shape (chosen for shell-safety + minimal escaping):

  gaia chain resume <token> --decision modify \
       --modify-step wait-checks \
       --modify-vars timeout=10m,branch=main

Only the yielded step's id may appear in --modify-step. Other
steps' args are part of the frozen chain spec and aren't
adjustable mid-flight; agents that want to change those should
abort + start a fresh chain with new --var inputs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := resolveStateDir()
			if err != nil {
				return err
			}
			runOpts := chain.RunOptions{
				StateDir:         stateDir,
				SubChainResolver: subChainResolver(),
			}
			if decision == "modify" {
				if modifyStep == "" {
					return exitcode.Errorf(exitcode.Usage, "--decision modify requires --modify-step <step-id>")
				}
				vars, err := parseVarFlags(modifyVars)
				if err != nil {
					return err
				}
				runOpts.Modify = &chain.ModifyDirective{StepID: modifyStep, Vars: vars}
			}
			res, err := chain.Resume(cmd.Context(), args[0], decision, runOpts)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Usage, "resume chain")
			}
			if err := renderEnvelope(cmd, flags, res, nil, nil); err != nil {
				return err
			}
			return chainExitFromStatus(res)
		},
	}
	cmd.Flags().StringVar(&decision, "decision", "continue", "continue, abort, or modify")
	cmd.Flags().StringVar(&modifyStep, "modify-step", "", "step id to modify (must match the yielded step; required for --decision modify)")
	cmd.Flags().StringSliceVar(&modifyVars, "modify-vars", nil, "var overrides as key=value (repeatable; required for --decision modify)")
	return cmd
}

func newChainListCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List chains that yielded but haven't been resumed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stateDir, err := resolveStateDir()
			if err != nil {
				return err
			}
			infos, err := chain.ListStates(stateDir)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Generic, "list chains")
			}
			return renderEnvelope(cmd, flags, infos, nil, prettyChainList)
		},
	}
}

func newChainAbortCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "abort <token>",
		Short: "Discard a yielded chain (alias for resume --decision abort)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stateDir, err := resolveStateDir()
			if err != nil {
				return err
			}
			res, err := chain.Resume(cmd.Context(), args[0], "abort", chain.RunOptions{StateDir: stateDir})
			if err != nil {
				return exitcode.Wrap(err, exitcode.Usage, "abort chain")
			}
			return renderEnvelope(cmd, flags, res, nil, nil)
		},
	}
	return cmd
}

// chainExitFromStatus translates a chain Result into the right
// process exit code. success → 0, yielded → 0 (the chain is alive,
// just paused; agents read the envelope), failure/aborted → 1.
func chainExitFromStatus(res *chain.Result) error {
	switch res.Status {
	case chain.StatusSuccess, chain.StatusYielded:
		return nil
	case chain.StatusFailure:
		return exitcode.Errorf(exitcode.Generic, "chain %q failed at step %q", res.Chain, res.FailedStep)
	case chain.StatusAborted:
		return exitcode.Errorf(exitcode.Generic, "chain %q aborted (reason: %s)", res.Chain, res.AbortReason)
	default:
		return nil
	}
}

// prettyChainList renders the chain list as a small table for
// `--format pretty`. JSON output (the default) goes through the
// envelope unchanged.
func prettyChainList(w io.Writer, data any) error {
	infos, ok := data.([]chain.StateInfo)
	if !ok {
		return fmt.Errorf("prettyChainList: unexpected type %T", data)
	}
	if len(infos) == 0 {
		_, _ = fmt.Fprintln(w, "(no yielded chains)")
		return nil
	}
	for _, i := range infos {
		_, _ = fmt.Fprintf(w, "%s  %s\n", i.Token, i.ModTime.Format(time.RFC3339))
	}
	return nil
}

// parseVarFlags converts a slice of "key=value" strings into a map.
// First '=' splits; values may contain further '=' chars.
func parseVarFlags(in []string) (map[string]string, error) {
	out := map[string]string{}
	for _, raw := range in {
		idx := strings.IndexByte(raw, '=')
		if idx < 1 {
			return nil, exitcode.Errorf(exitcode.Usage,
				"--var must be key=value (got %q)", raw)
		}
		key := raw[:idx]
		val := raw[idx+1:]
		if !looksLikeIdent(key) {
			return nil, exitcode.Errorf(exitcode.Usage,
				"--var key %q must be [A-Za-z_][A-Za-z0-9_]*", key)
		}
		out[key] = val
	}
	return out, nil
}

func looksLikeIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r == '_':
			// fine
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
