package types

import "time"

// Milestone is the trimmed view of a forge milestone. Both Forgejo
// and GitHub expose milestones with nearly the same shape; the
// JSON fields here line up with both wire formats.
//
// ID is preserved on this type (unlike Issue.Number / Release.TagName
// which serve as the user-facing handle) because Forgejo's PATCH and
// DELETE endpoints take the numeric ID — and milestone titles are
// not guaranteed unique across the historical set, so the ID is the
// only stable lookup key.
//
// Fields carrying operator-supplied content (Title, Description) are
// tagged `gaia:"trust=external"`; the envelope marshaler rewrites
// them on the wire as `{"_trust":"external","_value":...}` so agents
// can distinguish operator input from attacker-controllable data
// (#146).
//
// OpenIssues + ClosedIssues are forge-computed rollups — useful for
// agents triaging sprint progress without a second `gaia issue list`
// round-trip.
type Milestone struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title" gaia:"trust=external"`
	Description  string     `json:"description,omitempty" gaia:"trust=external"`
	State        string     `json:"state"`
	DueOn        *time.Time `json:"due_on,omitempty"`
	OpenIssues   int        `json:"open_issues"`
	ClosedIssues int        `json:"closed_issues"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
}
