package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/envelope"
	"github.com/stewartbrothers/gaia/core/provider"
)

// prettyFunc is the per-command human-readable renderer. nil means
// the command falls back to JSON regardless of --format.
type prettyFunc func(io.Writer, any) error

// renderEnvelope wraps data in the standard envelope, applies any
// flag-supplied projection, and writes either indented JSON or the
// command-supplied pretty rendering to cmd.OutOrStdout().
//
// page may be nil for non-list commands.
func renderEnvelope(cmd *cobra.Command, flags *globalFlags, data any, page *provider.Page, pretty prettyFunc) error {
	if flags.Format == "pretty" && pretty != nil {
		return pretty(cmd.OutOrStdout(), data)
	}

	env := envelope.New(data).WithPage(page)
	if flags.Fields != "" {
		if err := env.Project(flags.Fields); err != nil {
			return err
		}
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return fmt.Errorf("encode envelope: %w", err)
	}
	return nil
}
