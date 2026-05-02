package github_test

import (
	"context"
	"testing"

	"github.com/stewartbrothers/gaia/core/provider"
)

// Fixture-driven tests across the remaining read methods. Each test
// pins a specific contract from the trim layer + parity matrix; if
// the captured wire shape changes (fixture re-record), the assertion
// catches it loudly instead of the trim silently dropping a field.

func TestGetIssueFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-issue-1.json",
	})

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "cli", "cli", 1, provider.GetIssueOptions{})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Number != 1 {
		t.Errorf("Number: got %d, want 1", got.Number)
	}
	if got.State != "closed" {
		t.Errorf("State: got %q, want closed", got.State)
	}
	if got.Author.Login == "" {
		t.Error("Author.Login empty")
	}
}

func TestGetIssueWithCommentsFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"/repos/cli/cli/issues/1/comments": "cli-cli-comments-issue.json",
		"":                                 "cli-cli-issue-1.json",
	})

	p := newTestProvider(t, srv.URL)
	got, err := p.GetIssue(context.Background(), "cli", "cli", 1, provider.GetIssueOptions{
		WithComments: 50,
	})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Number != 1 {
		t.Errorf("Number: %d", got.Number)
	}
	if len(got.Comments) == 0 {
		t.Error("expected comments inlined from fixture")
	}
	for i, c := range got.Comments {
		if c.Author.Login == "" || c.Body == "" {
			t.Errorf("comment[%d] incomplete: %+v", i, c)
		}
	}
}

func TestListPullRequestsFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-pulls-list.json",
	})

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListPullRequests(context.Background(), "cli", "cli", provider.ListPullRequestsOptions{
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("count: %d, want 3", len(got))
	}
	for i, pr := range got {
		if pr.Number == 0 {
			t.Errorf("[%d] missing Number", i)
		}
		if pr.Head.Ref == "" {
			t.Errorf("[%d] missing Head.Ref", i)
		}
		if pr.Base.Ref == "" {
			t.Errorf("[%d] missing Base.Ref", i)
		}
	}
}

func TestGetPullRequestFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-pull-1.json",
	})

	p := newTestProvider(t, srv.URL)
	got, err := p.GetPullRequest(context.Background(), "cli", "cli", 1, provider.GetPullRequestOptions{})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if got.Number != 1 {
		t.Errorf("Number: %d", got.Number)
	}
	// PR #1 in cli/cli is merged. State reconciliation at the
	// provider boundary maps merged_at→"merged" — this is the
	// behavior callers rely on.
	if got.State != "merged" {
		t.Errorf("State: got %q, want merged (PR was merged)", got.State)
	}
}

func TestListReleasesFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-releases-list.json",
	})

	p := newTestProvider(t, srv.URL)
	got, _, err := p.ListReleases(context.Background(), "cli", "cli", provider.ListReleasesOptions{
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("ListReleases: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("count: %d, want 3", len(got))
	}
	for i, r := range got {
		if r.TagName == "" {
			t.Errorf("[%d] missing TagName: %+v", i, r)
		}
	}
}

func TestGetReleaseFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-release-tag.json",
	})

	p := newTestProvider(t, srv.URL)
	got, err := p.GetRelease(context.Background(), "cli", "cli", "v2.79.0")
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got.TagName != "v2.79.0" {
		t.Errorf("TagName: %q", got.TagName)
	}
	if got.PublishedAt == nil || got.PublishedAt.IsZero() {
		t.Error("PublishedAt zero")
	}
}

func TestSearchFromFixture(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"": "cli-cli-search-issues.json",
	})

	p := newTestProvider(t, srv.URL)
	got, _, err := p.Search(context.Background(), "label:bug", provider.SearchOptions{
		Repo:  "cli/cli",
		Kinds: []string{"issue"},
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected matches in fixture")
	}
	for i, r := range got {
		if r.Number == 0 {
			t.Errorf("[%d] missing Number", i)
		}
		if r.Title == "" {
			t.Errorf("[%d] missing Title", i)
		}
		if r.RepoFull == "" {
			t.Errorf("[%d] missing RepoFull (search trims this from html_url)", i)
		}
		if r.Kind == "" {
			t.Errorf("[%d] missing Kind", i)
		}
	}
}
