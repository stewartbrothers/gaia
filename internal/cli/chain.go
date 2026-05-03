package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/chain"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

func newChainCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chain",
		Short: "Run a chain of steps with success/failure routing",
		Long: `Chains let you describe a multi-step workflow once and have
gaia run it in one CLI invocation, returning a single envelope
with success/failure routing.

Phase A scope (current): linear chains via --chain-file. Phase B
will add saved chains in .gaia/chains/ and named chain
composition. Phase C adds parallel steps + retries.

See docs/chain.md for the YAML schema and examples.`,
	}
	cmd.AddCommand(newChainRunCmd(flags))
	return cmd
}

func newChainRunCmd(flags *globalFlags) *cobra.Command {
	var (
		chainFile string
		varFlags  []string
		dryRun    bool
		verbose   bool
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a chain from a YAML file",
		Long: `Run a chain definition.

  gaia chain run --chain-file ci.yaml \
    --var title="feat: thing" \
    --var body="description" \
    [--dry-run]

--var is repeatable; values containing '=' split on the first one
only ('--var msg=a=b' → key=msg, value=a=b).

Exit codes:
  0  chain succeeded
  1  chain failed (Result.Failure has details)
  2  usage error (bad flags, missing chain file, var validation)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if chainFile == "" {
				return exitcode.Errorf(exitcode.Usage, "--chain-file is required")
			}
			c, err := chain.ParseFile(chainFile)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Usage, "load chain")
			}

			vars, err := parseVarFlags(varFlags)
			if err != nil {
				return err
			}

			opts := chain.RunOptions{DryRun: dryRun}
			if verbose {
				opts.Progress = cmd.ErrOrStderr()
			}

			res, err := chain.Run(cmd.Context(), c, vars, opts)
			if err != nil {
				return exitcode.Wrap(err, exitcode.Usage, "run chain")
			}

			// Emit the envelope on stdout regardless of success/failure
			// — agents read the same shape either way and branch on
			// status.
			if err := renderEnvelope(cmd, flags, res, nil, nil); err != nil {
				return err
			}

			// Non-zero exit on failure so `gaia chain run ... && next` works.
			if res.Status == chain.StatusFailure {
				return exitcode.Errorf(exitcode.Generic, "chain %q failed at step %q", res.Chain, res.FailedStep)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&chainFile, "chain-file", "", "path to a chain YAML definition (required)")
	cmd.Flags().StringSliceVar(&varFlags, "var", nil, "chain input as key=value (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "substitute vars + render the resolved plan, but don't execute")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream per-step progress to stderr while the chain runs")
	return cmd
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
