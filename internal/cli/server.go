package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/types"
)

func newServerCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Inspect the forge server",
	}
	cmd.AddCommand(newServerVersionCmd(flags))
	return cmd
}

func newServerVersionCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the forge instance's version string",
		Long: `Hits the configured forge's version endpoint and prints the server
version. Useful for diagnostics and API compatibility checks after
an instance upgrade.

  gaia server version`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			sv, err := p.ServerVersion(cmd.Context())
			if err != nil {
				return err
			}
			return renderEnvelope(cmd, flags, sv, nil, prettyServerVersion)
		},
	}
}

func prettyServerVersion(w io.Writer, data any) error {
	sv, ok := data.(*types.ServerVersion)
	if !ok {
		return fmt.Errorf("prettyServerVersion: unexpected type %T", data)
	}
	_, err := fmt.Fprintln(w, sv.Version)
	return err
}
