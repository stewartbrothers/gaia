package types

// SearchResult is a hit from Provider.Search. Kind takes one of "issue",
// "pull_request", "code". RepoFull is the owner/name slug; populated even
// for same-repo searches so cross-repo result sets stay unambiguous.
type SearchResult struct {
	Kind     string `json:"kind"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
	RepoFull string `json:"repo"`
}
