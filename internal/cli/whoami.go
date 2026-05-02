package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type whoamiResult struct {
	Login    string `json:"login"`
	Provider string `json:"provider"`
	Host     string `json:"host"`
}

func newWhoamiCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Print the authenticated user's login",
		Long: `Calls the active provider's /user endpoint and prints the resulting
login. Confirms that the token currently configured for this provider
still works. Exit code 4 (Auth) means the token is missing or rejected.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, info, err := buildForgejoProvider(flags)
			if err != nil {
				return err
			}
			login, err := p.Whoami(cmd.Context())
			if err != nil {
				return err
			}
			data := whoamiResult{
				Login:    login,
				Provider: info.Provider,
				Host:     info.Host,
			}
			return renderEnvelope(cmd, flags, data, nil, prettyWhoami)
		},
	}
}

func prettyWhoami(w io.Writer, data any) error {
	r, ok := data.(whoamiResult)
	if !ok {
		return fmt.Errorf("prettyWhoami: unexpected type %T", data)
	}
	_, err := fmt.Fprintln(w, r.Login)
	return err
}
