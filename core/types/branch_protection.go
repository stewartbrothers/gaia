package types

// BranchProtection is the trimmed view of a branch's protection rule.
// gaia models the binding-relevant fields that map cleanly across forges
// — required status checks, the strict "branch must be up to date"
// toggle, and the required-approval count. Richer per-forge knobs (push
// restrictions, dismiss-stale-approvals, code-owner review, signed
// commits, GitHub's enforce-admins) are intentionally omitted from the
// trimmed shape; they can join when a real workflow needs them. (#345)
//
// The required-status-check contexts are the load-bearing field: a
// branch that requires a named check can't merge while that check is red
// AND can't merge while it is absent (a never-reported required check is
// not "satisfied"), which is what makes a CI gate binding rather than
// advisory.
type BranchProtection struct {
	// Branch is the branch name or pattern the rule applies to.
	Branch string `json:"branch"`
	// RequiredStatusChecks are the check context strings that must pass
	// before merge (e.g. "CI / Build"). Empty means no status-check
	// requirement. Pair with `gaia pr view --with-ci` to read the exact
	// context names (#344).
	RequiredStatusChecks []string `json:"required_status_checks,omitempty"`
	// StrictStatusChecks requires the branch to be up to date with base
	// before the required checks count as satisfied.
	StrictStatusChecks bool `json:"strict_status_checks"`
	// RequiredApprovals is the number of approving reviews required to
	// merge. 0 means no review requirement.
	RequiredApprovals int `json:"required_approvals"`
}
