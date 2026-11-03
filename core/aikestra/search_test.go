package aikestra

import (
	"context"
	"testing"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
)

// Search is Aikestra's Issue title substring search within one Project
// (briefing v1: GET /projects/{slug}/issues?q=). It searches every
// state, like a forge search, and projects only what a SearchResult
// needs.
func TestSearch_TitleSubstringInOneProject(t *testing.T) {
	got, page, err := replay(t, "issue_search").Search(context.Background(), "website", provider.SearchOptions{Repo: "acme/rocket", Limit: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || page.Truncated {
		t.Fatalf("got %+v page %+v", got, page)
	}
	if r := got[0]; r.Kind != "issue" || r.Number != 12 || r.Title != "Website contact form sends no email" || r.RepoFull != "acme" {
		t.Errorf("first = %+v", r)
	}
}

// Searching across Projects, or for pull requests, is not-implemented
// (briefing v1) and is refused before any request.
func TestSearch_Refused(t *testing.T) {
	p := replay(t)
	ctx := context.Background()
	_, _, err := p.Search(ctx, "x", provider.SearchOptions{})
	wantCode(t, err, exitcode.NotImplemented, "across Projects")
	_, _, err = p.Search(ctx, "x", provider.SearchOptions{Repo: "acme/rocket", Kinds: []string{"pull_request"}})
	wantCode(t, err, exitcode.NotImplemented, "pull")
}

func TestSearch_IssueKindAllowed(t *testing.T) {
	_, _, err := replay(t, "issue_search").Search(context.Background(), "website", provider.SearchOptions{Repo: "acme/rocket", Kinds: []string{"issue"}, Limit: 30})
	if err != nil {
		t.Fatal(err)
	}
}
