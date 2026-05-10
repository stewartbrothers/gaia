// Package agentguide hosts the anti-rot logic that keeps
// docs/agent-guide.md from silently falling behind the CLI command
// surface. The exported helpers walk a cobra command tree, collect
// the set of top-level commands worth advertising to agents, and
// report which of those commands are not mentioned anywhere in the
// guide.
//
// The logic lives apart from the test that drives it so it is unit
// testable against fixture cobra trees and fixture markdown — the
// guard against the test silently passing because of a bug in the
// test itself. See coverage_unit_test.go for those self-tests, and
// coverage_test.go for the real-world assertion against
// internal/cli.NewRootCmd() and docs/agent-guide.md.
package agentguide

import (
	"strings"

	"github.com/spf13/cobra"
)

// metaCommands is the allowlist of cobra-auto-generated commands and
// other meta entries that carry no agent-facing surface and so do
// not need to be mentioned in the guide. Kept tiny on purpose: any
// real top-level command must appear in the guide.
var metaCommands = map[string]struct{}{
	"help":       {},
	"completion": {},
}

// TopLevelCommands returns the set of top-level command names from
// root that are subject to the agent-guide coverage rule. Hidden
// commands and the meta allowlist (help, completion) are skipped.
//
// The returned slice is sorted (stable test output) and contains
// each command's primary name only — aliases are not enumerated, so
// docs are not asked to mention every alias the cobra tree exposes.
func TopLevelCommands(root *cobra.Command) []string {
	if root == nil {
		return nil
	}
	out := make([]string, 0, len(root.Commands()))
	for _, c := range root.Commands() {
		if c.Hidden {
			continue
		}
		name := c.Name()
		if _, skip := metaCommands[name]; skip {
			continue
		}
		out = append(out, name)
	}
	// Cobra already returns commands sorted alphabetically by Name,
	// but make it explicit so a future cobra change doesn't surprise
	// us.
	sortStrings(out)
	return out
}

// MissingFromGuide returns the subset of commands whose `gaia <cmd>`
// token does not appear anywhere in guide. Substring matching is
// the bar — the test asserts presence, not depth. Any deeper quality
// review belongs in human PR review, not this anti-rot test.
//
// The token format is `gaia <command>` with a single space — the
// way the guide naturally reads. This deliberately matches both
// `gaia issue` and `gaia issue create` in prose, so a guide that
// mentions a subcommand counts as covering its parent.
func MissingFromGuide(commands []string, guide string) []string {
	if len(commands) == 0 {
		return nil
	}
	var missing []string
	for _, cmd := range commands {
		token := "gaia " + cmd
		if !strings.Contains(guide, token) {
			missing = append(missing, cmd)
		}
	}
	return missing
}

// sortStrings is a tiny helper to keep the import surface minimal.
// The standard library sort would do, but pulling it in for one
// call adds noise to the package-level imports.
func sortStrings(s []string) {
	// Insertion sort — n is at most ~20 (top-level commands).
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
