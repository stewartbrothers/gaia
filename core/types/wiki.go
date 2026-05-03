package types

import "time"

// WikiPage is the trimmed view of a forge wiki page. URLs and internal
// IDs are intentionally omitted — agents key off Path (the slug used in
// URLs and API calls) and Title (the human-readable heading).
//
// Body is the markdown source. List endpoints leave it empty so the
// caller-facing payload stays small; `gaia wiki view` populates it.
//
// LastCommit is the short SHA (7 chars) of the last commit that
// touched the page, useful for cache invalidation and "is this still
// the version I read?" checks. Forgejo returns the full SHA; we trim
// at the provider layer.
type WikiPage struct {
	Title      string    `json:"title" gaia:"trust=external"`
	Path       string    `json:"path"`
	Body       string    `json:"body,omitempty" gaia:"trust=external"`
	LastCommit string    `json:"last_commit,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// WikiSearchHit is one search result from `gaia wiki search`. Snippet
// is a window of ~200 chars centred on the first match in either the
// title or the body, with the matched substring left intact for the
// caller to highlight as it sees fit.
//
// Title and Snippet are forge-supplied user content; tagged
// `gaia:"trust=external"` for the indirect-prompt-injection
// mitigation (#146).
type WikiSearchHit struct {
	Path    string `json:"path"`
	Title   string `json:"title" gaia:"trust=external"`
	Snippet string `json:"snippet,omitempty" gaia:"trust=external"`
}
