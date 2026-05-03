package types

// SearchResult is a hit from Provider.Search. Kind takes one of "issue",
// "pull_request", "code". RepoFull is the owner/name slug; populated even
// for same-repo searches so cross-repo result sets stay unambiguous.
//
// Title is the forge-supplied issue/PR/file title and is tagged
// `gaia:"trust=external"` for the indirect-prompt-injection
// mitigation (#146).
type SearchResult struct {
	Kind     string `json:"kind"`
	Number   int    `json:"number"`
	Title    string `json:"title" gaia:"trust=external"`
	RepoFull string `json:"repo"`
}
