package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newAuthCmd is the parent for all `gaia auth ...` subcommands. It
// has no behavior of its own — running `gaia auth` with no arg shows
// help.
func newAuthCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate gaia against a forge",
		Long: `Manage credentials for forge instances. After running an auth
subcommand, gaia commands work without --token, --api-url, or
FORGEJO_TOKEN/GITHUB_TOKEN env vars (the credential is recorded in
~/.config/gaia/credentials.yaml or, with --project, in
.gaia/credentials.yaml inside the current repo).`,
	}
	cmd.AddCommand(newAuthForgejoCmd())
	cmd.AddCommand(newAuthGHCmd())
	cmd.AddCommand(newAuthStatusCmd(flags))
	cmd.AddCommand(newAuthLogoutCmd())
	return cmd
}

// readSecret reads a single line of input from in, masking the
// terminal when in is a TTY. The prompt is written to promptW first.
// Returns the trimmed token or an error.
func readSecret(in io.Reader, promptW io.Writer, prompt string) (string, error) {
	_, _ = fmt.Fprint(promptW, prompt)

	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		raw, err := term.ReadPassword(int(f.Fd()))
		_, _ = fmt.Fprintln(promptW)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", nil
	}
	return strings.TrimSpace(scanner.Text()), nil
}
