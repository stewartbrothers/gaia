//go:build integration

// Package-level integration tests. Run with:
//
//   go test -tags integration -run Live ./core/github -count=1 -v
//
// Hits real api.github.com. No token needed for the public-repo
// reads; the test exercises the trim contract against actual GitHub
// responses (which are richer than our fixtures), so a regression in
// the apiX shape becomes a test failure rather than a runtime
// surprise.
//
// Set GITHUB_TOKEN to lift the 60 req/hour anonymous rate limit.

package github_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/github"
	"github.com/stewartbrothers/gaia/core/provider"
)

const liveRepo = "cli/cli" // public, large, stable; many open issues + PRs

func liveProvider(t *testing.T) *github.Provider {
	t.Helper()
	return github.NewProvider(github.Options{
		Token:     os.Getenv("GITHUB_TOKEN"), // empty is OK for public reads
		UserAgent: "gaia-integration-test/1.0",
	})
}

func TestLiveListIssuesPublicRepo(t *testing.T) {
	p := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	issues, page, err := p.ListIssues(ctx, "cli", "cli", provider.ListIssuesOptions{
		State: "open",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) == 0 {
		t.Fatalf("expected some open issues on cli/cli; got 0")
	}
	for _, i := range issues {
		// Trim contract: every required field decoded.
		if i.Number == 0 || i.Title == "" || i.State == "" || i.Author.Login == "" {
			t.Errorf("incomplete decode: %+v", i)
		}
		if i.CreatedAt.IsZero() || i.UpdatedAt.IsZero() {
			t.Errorf("timestamps not parsed: %+v", i)
		}
	}
	t.Logf("got %d open issues; page=%+v; first=#%d %q",
		len(issues), page, issues[0].Number, issues[0].Title)
}

func TestLiveGetIssuePublicRepo(t *testing.T) {
	p := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// cli/cli #1 should exist for any healthy repo (and on cli/cli it
	// does — the first issue ever filed). Stable target.
	got, err := p.GetIssue(ctx, "cli", "cli", 1, provider.GetIssueOptions{})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Number != 1 {
		t.Errorf("number: %d", got.Number)
	}
	if got.Title == "" || got.Author.Login == "" {
		t.Errorf("decode: %+v", got)
	}
	t.Logf("issue #1: %q by @%s, state=%s",
		got.Title, got.Author.Login, got.State)
}

func TestLiveListPullRequestsPublicRepo(t *testing.T) {
	p := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prs, _, err := p.ListPullRequests(ctx, "cli", "cli", provider.ListPullRequestsOptions{
		State: "open",
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) == 0 {
		t.Skip("no open PRs on cli/cli right now (unusual but possible)")
	}
	for _, pr := range prs {
		if pr.Number == 0 || pr.Title == "" || pr.Head.SHA == "" || pr.Base.Ref == "" {
			t.Errorf("incomplete PR decode: %+v", pr)
		}
		// State reconciliation: real GitHub PRs should be open|closed|merged.
		if pr.State != "open" && pr.State != "closed" && pr.State != "merged" {
			t.Errorf("unexpected state %q on #%d", pr.State, pr.Number)
		}
	}
	t.Logf("got %d open PRs; first=#%d %q (head=%s base=%s)",
		len(prs), prs[0].Number, prs[0].Title, prs[0].Head.Ref, prs[0].Base.Ref)
}

func TestLivePullRequestDiffPublicRepo(t *testing.T) {
	p := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find an open PR, fetch its diff, assert the parser handled the
	// real (probably-large) response. cli/cli has churn; pick the
	// freshest open PR instead of pinning a number.
	prs, _, err := p.ListPullRequests(ctx, "cli", "cli", provider.ListPullRequestsOptions{
		State: "open",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) == 0 {
		t.Skip("no open PR to diff against")
	}

	files, err := p.GetPullRequestDiff(ctx, "cli", "cli", prs[0].Number, provider.GetPullRequestDiffOptions{})
	if err != nil {
		t.Fatalf("GetPullRequestDiff #%d: %v", prs[0].Number, err)
	}
	t.Logf("PR #%d diff: %d files", prs[0].Number, len(files))
	if len(files) == 0 {
		// Some PRs are doc-only or empty — skip rather than fail.
		t.Skip("PR has 0 files; nothing to assert")
	}
	if files[0].Path == "" || files[0].Status == "" {
		t.Errorf("incomplete file decode: %+v", files[0])
	}
}

func TestLiveWhoamiRequiresToken(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		t.Skip("set GITHUB_TOKEN to exercise Whoami")
	}
	p := liveProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	login, err := p.Whoami(ctx)
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if login == "" {
		t.Errorf("login empty")
	}
	t.Logf("authenticated as: %s", login)
}
