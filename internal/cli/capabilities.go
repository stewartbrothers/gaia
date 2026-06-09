package cli

import (
	"github.com/spf13/cobra"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// capabilityAnnotation is the cobra annotation key carrying the
// [provider.Capability] that gates a resource command group.
const capabilityAnnotation = "gaia_capability"

// annotateCapability tags a resource group command with the capability
// that gates it, so [capabilityGuard] can block it when the active
// provider declares that capability unsupported.
func annotateCapability(cmd *cobra.Command, cap provider.Capability) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[capabilityAnnotation] = string(cap)
	return cmd
}

// gatedCapability walks cmd and its ancestors for a capability
// annotation, returning the first found (so `gaia wiki list` inherits
// the gate from its `wiki` parent).
func gatedCapability(cmd *cobra.Command) (provider.Capability, bool) {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations != nil {
			if v, ok := c.Annotations[capabilityAnnotation]; ok {
				return provider.Capability(v), true
			}
		}
	}
	return "", false
}

// capabilityGuard returns a PersistentPreRunE that blocks a
// capability-gated command when the active provider declares it
// unsupported (#342). It is deliberately cheap and best-effort:
//
//   - A command with no capability annotation (version, auth, issue,
//     label, …) returns immediately, resolving no settings — so meta and
//     offline commands pay nothing and never need a configured provider.
//   - If the provider name can't be resolved (unconfigured) or the
//     provider isn't registered, the guard does not block — the command
//     proceeds and surfaces its own error.
//
// Real forges (Forgejo, GitHub) declare no unsupported capabilities, so
// this never blocks anything in production; it is the seam a future
// asymmetric provider (e.g. an issues-only backend) uses to keep
// operators from invoking commands it can't serve.
func capabilityGuard(flags *globalFlags) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cap, gated := gatedCapability(cmd)
		if !gated {
			return nil
		}
		s, err := loadSettings(flags)
		if err != nil {
			return nil //nolint:nilerr // can't resolve provider → don't block; command surfaces its own error
		}
		name := s.Provider()
		if name == "" || provider.Supports(name, cap) {
			return nil
		}
		return exitcode.Errorf(exitcode.Usage,
			"the %s provider does not support %s", name, cap)
	}
}
