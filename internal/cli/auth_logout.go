package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/auth"
	"github.com/stewartbrothers/gaia/core/exitcode"
)

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [provider | provider:host]",
		Short: "Remove a stored credential",
		Long: `Removes the credential for the named provider (or provider:host).
With no arg, lists all configured credentials and prompts for a
selection.

Behavior:

  - "forgejo:git.example.com"  → exact match, removed without prompt.
  - "forgejo"                  → if exactly one forgejo credential, removed.
                                  Otherwise lists matches and prompts.
  - (no arg)                   → lists all credentials and prompts.

Removing the only credential of a given provider:host pair is a
no-op when the pair doesn't exist; that's NOT an error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := collectCredentialEntries()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no credentials stored")
				return nil
			}

			var pick credEntry
			switch {
			case len(args) == 0:
				pick, err = pickInteractive(cmd, entries)
			case strings.Contains(args[0], ":"):
				pick, err = pickExact(args[0], entries)
			default:
				pick, err = pickByProvider(cmd, args[0], entries)
			}
			if err != nil {
				return err
			}

			path, err := credentialPathForSource(pick.Source)
			if err != nil {
				return err
			}
			store, err := auth.Load(path)
			if err != nil {
				return err
			}
			store.Remove(pick.Provider, pick.Host)
			if err := auth.Save(path, store); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed %s:%s from %s.\n", pick.Provider, pick.Host, path)
			return nil
		},
	}
}

func pickExact(target string, entries []credEntry) (credEntry, error) {
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 {
		return credEntry{}, exitcode.Errorf(exitcode.Usage, "expected provider:host, got %q", target)
	}
	for _, e := range entries {
		if e.Provider == parts[0] && e.Host == parts[1] {
			return e, nil
		}
	}
	return credEntry{}, exitcode.Errorf(exitcode.NotFound, "no credential for %s", target)
}

func pickByProvider(cmd *cobra.Command, provider string, entries []credEntry) (credEntry, error) {
	matches := []credEntry{}
	for _, e := range entries {
		if e.Provider == provider {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return credEntry{}, exitcode.Errorf(exitcode.NotFound, "no credential for provider %s", provider)
	case 1:
		return matches[0], nil
	default:
		return pickInteractive(cmd, matches)
	}
}

func pickInteractive(cmd *cobra.Command, entries []credEntry) (credEntry, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, "Which credential should I remove?")
	for i, e := range entries {
		_, _ = fmt.Fprintf(out, "  [%d] %s:%s (%s)\n", i+1, e.Provider, e.Host, e.Source)
	}
	_, _ = fmt.Fprint(out, "> ")

	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return credEntry{}, exitcode.Errorf(exitcode.Usage, "no selection (stdin closed)")
	}
	choice := strings.TrimSpace(scanner.Text())
	n, err := strconv.Atoi(choice)
	if err != nil || n < 1 || n > len(entries) {
		return credEntry{}, exitcode.Errorf(exitcode.Usage, "invalid selection %q (expected 1..%d)", choice, len(entries))
	}
	return entries[n-1], nil
}

// credentialPathForSource returns the file path for a "global" or
// "project" source label.
func credentialPathForSource(source string) (string, error) {
	switch source {
	case "project":
		root := auth.ProjectRoot(".")
		if root == "" {
			return "", exitcode.Errorf(exitcode.Generic, "project credential outside a git repo (cwd changed since auth?)")
		}
		return auth.ProjectPath(root), nil
	default:
		return auth.DefaultGlobalPath()
	}
}
