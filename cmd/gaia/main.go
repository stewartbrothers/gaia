// Command gaia is the user-facing CLI for the gaia toolkit. It is
// intentionally a thin shim: every command's behavior lives in
// internal/cli, and core/* holds the shared library. Errors propagate
// from internal/cli as exitcode.Error values; this main translates
// them into the process exit code.
package main

import (
	"fmt"
	"os"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gaia: "+err.Error())
		os.Exit(exitcode.Of(err))
	}
}
