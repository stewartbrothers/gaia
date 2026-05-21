package types

import "time"

// PullRequest is the trimmed view of a pull request. State takes one of
// "open", "closed", "merged" — Forgejo reports closed-without-merge and
// merged separately, and the consolidated value is more useful to agents
// than reconstructing it from {state,merged_at}.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title" gaia:"trust=external"`
	State     string    `json:"state"`
	Author    User      `json:"author"`
	Labels    []Label   `json:"labels,omitempty"`
	Head      BranchRef `json:"head"`
	Base      BranchRef `json:"base"`
	Mergeable *bool     `json:"mergeable,omitempty"`
	Draft     bool      `json:"draft"`
	// HTMLURL points at the PR's UI page. Used by agents to redirect
	// humans to the forge (sharing, review, merge UI) without
	// reconstructing the URL from API-base + owner/repo/number.
	// Mirrors Issue.HTMLURL and WorkflowRun.HTMLURL (#305).
	HTMLURL   string     `json:"html_url,omitempty"`
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
//
// Checks, when populated, lists the individual check name + state
// pairs that compose the rollup. Populated by `gaia pr ci-wait` so
// chains can apply name-based flakiness heuristics; unpopulated by
// the default `gaia pr view --with-ci` path because most consumers
// only need the rollup. Always omitempty.
type CISummary struct {
	State      string      `json:"state"`
	Total      int         `json:"total"`
	Successful int         `json:"successful"`
	Failed     int         `json:"failed"`
	Pending    int         `json:"pending"`
	Checks     []CheckItem `json:"checks,omitempty"`
}

// CheckItem is one individual CI check on a PR's head commit. Name
// is the upstream-reported context/check-name; State takes the same
// vocabulary as CISummary.State for the single check.
type CheckItem struct {
	Name  string `json:"name"`
	State string `json:"state"`
}
