package types

import "time"

// Issue is the trimmed view of a forge issue. PRs reuse this shape's
// fields where they overlap; see PullRequest for PR-specific extras.
type Issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Author    User       `json:"author"`
	Labels    []Label    `json:"labels,omitempty"`
	Assignees []User     `json:"assignees,omitempty"`
	Body      string     `json:"body,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	Comments  []Comment  `json:"comments,omitempty"`
}
