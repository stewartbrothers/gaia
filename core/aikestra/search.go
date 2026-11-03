package aikestra

import (
	"context"
	"net/url"
	"strings"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiSearchHit is the projection Search asks for (?fields=number,title,repo).
type apiSearchHit struct {
	Number int     `json:"number"`
	Title  string  `json:"title"`
	Repo   *string `json:"repo"`
}

// Search is Aikestra's case-insensitive Issue title substring search
// within one Project (GET /projects/{slug}/issues?q=), across every
// state. Searching across Projects, bodies, comments, or pull requests
// is not in API v1 and is refused rather than emulated. A hit's repo is
// "project/repo" when the Issue names a Repo, otherwise the Project
// slug alone: Issues belong to the Project, not to the --repo searched.
func (p *Provider) Search(ctx context.Context, query string, opts provider.SearchOptions) ([]types.SearchResult, *provider.Page, error) {
	project, _, ok := strings.Cut(opts.Repo, "/")
	if !ok || project == "" {
		return nil, nil, notImplemented("search across Projects; pass --repo <project>/<repo> to search one Project's Issue titles")
	}
	for _, k := range opts.Kinds {
		if k != "issue" {
			return nil, nil, exitcode.Errorf(exitcode.NotImplemented,
				"the aikestra provider does not support searching %s; only issue titles", k)
		}
	}
	q := url.Values{}
	q.Set("state", "all")
	q.Set("q", query)
	q.Set("fields", "number,title,repo")
	page, err := getPage[apiSearchHit](ctx, p.client, projectPath(project, "/issues"), q, opts.Limit, opts.Cursor)
	if err != nil {
		return nil, nil, err
	}
	out := make([]types.SearchResult, len(page.Items))
	for i, h := range page.Items {
		repo := project
		if h.Repo != nil && *h.Repo != "" {
			repo = project + "/" + *h.Repo
		}
		out[i] = types.SearchResult{Kind: "issue", Number: h.Number, Title: h.Title, RepoFull: repo}
	}
	return out, pageOf(page.NextCursor), nil
}
