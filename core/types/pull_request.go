package types

import "time"

// PullRequest is the trimmed view of a pull request. State takes one of
// "open", "closed", "merged" — Forgejo reports closed-without-merge and
// merged separately, and the consolidated value is more useful to agents
// than reconstructing it from {state,merged_at}.
type PullRequest struct {
	Number    int        `json:"number"`
	Title     string     `json:"title" gaia:"trust=external"`
	State     string     `json:"state"`
	Author    User       `json:"author"`
	Labels    []Label    `json:"labels,omitempty"`
	Head      BranchRef  `json:"head"`
	Base      BranchRef  `json:"base"`
	Mergeable *bool      `json:"mergeable,omitempty"`
	Draft     bool       `json:"draft"`
	Body      string     `json:"body,omitempty" gaia:"trust=external"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	MergedAt  *time.Time `json:"merged_at,omitempty"`
	CISummary *CISummary `json:"ci_summary,omitempty"`
	Comments  []Comment  `json:"comments,omitempty"`
}

// BranchRef points at one side of a PR. Repo is owner/name when the head
// is on a fork; empty when same-repo.
type BranchRef struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo string `json:"repo,omitempty"`
}

// CISummary is the rolled-up CI status across all checks for a PR's
// head commit. State takes one of "success", "pending", "failure",
// "error", "neutral".
type CISummary struct {
	State      string `json:"state"`
	Total      int    `json:"total"`
	Successful int    `json:"successful"`
	Failed     int    `json:"failed"`
	Pending    int    `json:"pending"`
}
