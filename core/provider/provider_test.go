package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// TestProviderInterfaceCompiles is a compile-time guard: it asserts that
// noopProvider satisfies provider.Provider. If a method signature drifts
// in either provider.go or any concrete implementation, this file stops
// compiling — the whole point is to catch interface drift at `go vet`
// time rather than at first integration.
func TestProviderInterfaceCompiles(t *testing.T) {
	var _ provider.Provider = (*noopProvider)(nil)
}

// TestPageZeroValueIsNotTruncated documents the zero-value contract:
// a Page returned by an implementation that fits everything in one call
// should be the zero value, which means Truncated=false and no cursor.
func TestPageZeroValueIsNotTruncated(t *testing.T) {
	var p provider.Page
	if p.Truncated || p.NextCursor != "" {
		t.Errorf("zero Page should be non-truncated and cursor-less; got %+v", p)
	}
}

// TestOptionsZeroValuesUsable asserts that every option struct is usable
// at its zero value — no required fields, no nil-map panics. Any agent
// that constructs an empty struct and calls through must not blow up.
func TestOptionsZeroValuesUsable(t *testing.T) {
	_ = provider.ListIssuesOptions{}
	_ = provider.GetIssueOptions{}
	_ = provider.ListPullRequestsOptions{}
	_ = provider.GetPullRequestOptions{}
	_ = provider.GetPullRequestDiffOptions{}
	_ = provider.ListCommentsOptions{}
	_ = provider.SearchOptions{}
}

// noopProvider is the always-fail stand-in used to compile-check the
// interface. It MUST NOT be used for any test that exercises behavior —
// real provider tests live alongside their implementations and use
// httptest.
type noopProvider struct{}

var errNotImplemented = errors.New("noop provider")

func (*noopProvider) Whoami(_ context.Context) (string, error) {
	return "", errNotImplemented
}
func (*noopProvider) ListIssues(_ context.Context, _, _ string, _ provider.ListIssuesOptions) ([]types.Issue, *provider.Page, error) {
	return nil, nil, errNotImplemented
}
func (*noopProvider) GetIssue(_ context.Context, _, _ string, _ int, _ provider.GetIssueOptions) (*types.Issue, error) {
	return nil, errNotImplemented
}
func (*noopProvider) ListPullRequests(_ context.Context, _, _ string, _ provider.ListPullRequestsOptions) ([]types.PullRequest, *provider.Page, error) {
	return nil, nil, errNotImplemented
}
func (*noopProvider) GetPullRequest(_ context.Context, _, _ string, _ int, _ provider.GetPullRequestOptions) (*types.PullRequest, error) {
	return nil, errNotImplemented
}
func (*noopProvider) GetPullRequestDiff(_ context.Context, _, _ string, _ int, _ provider.GetPullRequestDiffOptions) ([]types.DiffFile, error) {
	return nil, errNotImplemented
}
func (*noopProvider) ListComments(_ context.Context, _, _ string, _ int, _ provider.ListCommentsOptions) ([]types.Comment, error) {
	return nil, errNotImplemented
}
func (*noopProvider) Search(_ context.Context, _ string, _ provider.SearchOptions) ([]types.SearchResult, *provider.Page, error) {
	return nil, nil, errNotImplemented
}

// Reference time.Time so an unused-import check never fires while tests
// scaffold up. Removed once a real test uses it.
var _ = time.Now
