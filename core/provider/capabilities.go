package provider

// Capabilities describe, at a coarse resource-category granularity, what
// a provider supports *as a product*. This is static, compile-time
// knowledge: the Forgejo adapter is built knowing Forgejo has wikis; a
// future Linear adapter would be built knowing it has none. There is no
// runtime probe — a forge declares what it lacks in its registry
// [Registration.Unsupported], and consumers read it by provider name
// without building or calling a provider (#342, replacing the
// runtime-prober design in #310).
//
// Granularity is deliberately coarse — whole resource categories. A
// fine-grained gap *within* a supported category (a Forgejo server
// version missing one Actions endpoint; a token lacking a permission)
// stays on the per-call path: the method returns
// [exitcode.NotImplemented] or the API returns 403. The capability set
// answers "does this provider have wikis at all", not "can this exact
// call succeed right now".

// Capability is a resource category a provider may or may not support.
type Capability string

const (
	CapPullRequests Capability = "pull_requests"
	CapWikis        Capability = "wikis"
	CapReleases     Capability = "releases"
	CapWebhooks     Capability = "webhooks"
	CapPackages     Capability = "packages"
	CapActions      Capability = "actions"
	CapMilestones   Capability = "milestones"
)

// Supports reports whether the provider registered under name offers
// cap. The default is permissive: an unregistered name, or a forge that
// declared no Unsupported capabilities, supports everything. Only an
// explicit entry in [Registration.Unsupported] returns false. This keeps
// every forge that supports the full surface (Forgejo, GitHub)
// zero-config — they say nothing and nothing is hidden.
func Supports(name string, cap Capability) bool {
	reg, ok := Lookup(name)
	if !ok {
		return true
	}
	for _, u := range reg.Unsupported {
		if u == cap {
			return false
		}
	}
	return true
}

// UnsupportedCapabilities returns the capabilities the named provider
// declared it lacks, or nil for an unregistered name or a forge that
// supports everything.
func UnsupportedCapabilities(name string) []Capability {
	if reg, ok := Lookup(name); ok {
		return reg.Unsupported
	}
	return nil
}
