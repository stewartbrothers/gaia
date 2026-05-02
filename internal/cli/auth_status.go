package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/auth"
)

// credEntry is the per-credential row for `gaia auth status` output.
type credEntry struct {
	Provider string `json:"provider"`
	Host     string `json:"host"`
	User     string `json:"user,omitempty"`
	Source   string `json:"source"`
	TokenSet bool   `json:"token_set"`
}

func newAuthStatusCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List configured credentials (token values redacted)",
		Long: `Lists every credential configured globally
(~/.config/gaia/credentials.yaml) and per-project
(.gaia/credentials.yaml). Token values are NEVER printed; only
their presence is reported (token_set: true|false).

Empty store exits 0 with "no credentials stored".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			entries, err := collectCredentialEntries()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no credentials stored")
				return nil
			}
			return renderEnvelope(cmd, flags, entries, nil, prettyAuthStatus)
		},
	}
}

func prettyAuthStatus(w io.Writer, data any) error {
	entries, ok := data.([]credEntry)
	if !ok {
		return fmt.Errorf("prettyAuthStatus: unexpected type %T", data)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "PROVIDER\tHOST\tUSER\tSOURCE\tTOKEN")
	for _, e := range entries {
		tokenCol := "false"
		if e.TokenSet {
			tokenCol = "true"
		}
		user := e.User
		if user == "" {
			user = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Provider, e.Host, user, e.Source, tokenCol)
	}
	return tw.Flush()
}

// collectCredentialEntries walks both stores and returns one row per
// (provider, host) pair, project entries shadowing same-host global
// entries (the same precedence Layered.Get uses for lookups).
func collectCredentialEntries() ([]credEntry, error) {
	creds, err := loadLayeredCredentials()
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	out := []credEntry{}

	add := func(s *auth.Store, source string) {
		if s == nil {
			return
		}
		for _, key := range s.Hosts() {
			parts := splitProviderHost(key)
			if parts == nil {
				continue
			}
			pkey := parts[0] + ":" + parts[1]
			if seen[pkey] {
				continue
			}
			seen[pkey] = true
			c, _ := s.Get(parts[0], parts[1])
			out = append(out, credEntry{
				Provider: parts[0],
				Host:     parts[1],
				User:     c.User,
				Source:   source,
				TokenSet: c.Token != "",
			})
		}
	}
	add(creds.Project, "project")
	add(creds.Global, "global")

	return out, nil
}
