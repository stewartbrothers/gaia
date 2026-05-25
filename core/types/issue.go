package types

import "time"

// Issue is the trimmed view of a forge issue. PRs reuse this shape's
// fields where they overlap; see PullRequest for PR-specific extras.
//
// Fields carrying user-supplied forge content (Body, Title) are
// tagged `gaia:"trust=external"`; the envelope marshaler rewrites
// them on the wire as `{"_trust":"external","_value":...}` so
// agents can distinguish operator input from attacker-controllable
// data (#146).
type Issue struct {
	Number    int     `json:"number"`
	Title     string  `json:"title" gaia:"trust=external"`
	State     string  `json:"state"`
	Author    User    `json:"author"`
	Labels    []Label `json:"labels,omitempty"`
	Assignees []User  `json:"assignees,omitempty"`
	// HTMLURL points at the issue's UI page. Useful when an agent
	// needs to redirect a human to the forge (sharing, review) without
	// reconstructing the URL from API-base + owner/repo/number — that
	// reconstruction is brittle because it assumes the Forgejo /
	// GitHub URL convention holds. Mirrors WorkflowRun.HTMLURL (#305).
	HTMLURL   string     `json:"html_url,omitempty"`
	Body      string     `json:"body,omitempty" gaia:"trust=external"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	Comments  []Comment  `json:"comments,omitempty"`
	// Blockers are issues blocking this one (this issue depends on
	// them). Populated only when GetIssueOptions.WithBlockers > 0.
	// Each entry carries the trimmed Issue shape with Body="" and
	// no further nested Blockers/Blocks — the inline view is
	// one-level. See #317.
	Blockers []Issue `json:"blockers,omitempty"`
	// Blocks are issues this one is blocking. Populated only when
	// GetIssueOptions.WithBlocks > 0. Same trim contract as
	// Blockers.
	Blocks []Issue `json:"blocks,omitempty"`
}
