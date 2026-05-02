package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/internal/version"
)

type versionResult struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
}

func newVersionCmd() *cobra.Command {
	flags := &globalFlags{}

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print gaia version, commit, and Go runtime info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Pull format from the root's persistent flag rather than
			// re-binding it locally — keeps the pretty/json switch
			// consistent across all subcommands.
			if v, _ := cmd.Flags().GetString("format"); v != "" {
				flags.Format = v
			}
			data := versionResult{
				Version:   version.Version,
				Commit:    version.Commit,
				GoVersion: runtime.Version(),
			}
			return renderEnvelope(cmd, flags, data, nil, prettyVersion)
		},
	}
	return cmd
}

func prettyVersion(w io.Writer, data any) error {
	v, ok := data.(versionResult)
	if !ok {
		return fmt.Errorf("prettyVersion: unexpected type %T", data)
	}
	_, err := fmt.Fprintf(w, "gaia %s (commit %s, %s)\n", v.Version, v.Commit, v.GoVersion)
	return err
}
