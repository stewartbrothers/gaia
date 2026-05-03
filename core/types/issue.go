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
	Number    int        `json:"number"`
	Title     string     `json:"title" gaia:"trust=external"`
	State     string     `json:"state"`
	Author    User       `json:"author"`
	Labels    []Label    `json:"labels,omitempty"`
	Assignees []User     `json:"assignees,omitempty"`
	Body      string     `json:"body,omitempty" gaia:"trust=external"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	Comments  []Comment  `json:"comments,omitempty"`
}
