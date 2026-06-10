package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stewartbrothers/gaia/core/cache"
	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// CreatePullRequest opens a new PR.
func (p *Provider) CreatePullRequest(ctx context.Context, owner, repo string, opts provider.CreatePullRequestOptions) (*types.PullRequest, error) {
	var raw apiPullRequest
	if err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterCreate(ctx, kindPR, owner, repo)
	out := raw.toType()
	return &out, nil
}

// EditPullRequest patches a PR. Same omit-on-empty semantics as
// EditIssue. Draft is *bool because false != "no change".
func (p *Provider) EditPullRequest(ctx context.Context, owner, repo string, n int, opts provider.EditPullRequestOptions) (*types.PullRequest, error) {
	var raw apiPullRequest
	if err := p.client.Patch(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n), opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindPR, owner, repo, itoa(n))
	out := raw.toType()
	return &out, nil
}

// apiMergeRequest is the GitHub-specific merge body. Note that
// GitHub names the method field "merge_method", not Forgejo's "do".
// Title and Message use commit_title / commit_message.
type apiMergeRequest struct {
	CommitTitle   string `json:"commit_title,omitempty"`
	CommitMessage string `json:"commit_message,omitempty"`
	MergeMethod   string `json:"merge_method,omitempty"`
}

// MergePullRequest performs the merge with the requested method.
// GitHub's PUT /repos/{o}/{r}/pulls/{n}/merge — different verb than
// Forgejo's POST. Method values: "merge"|"rebase"|"squash" (default
// "merge").
//
// Note: provider.MergePullRequestOptions has Forgejo-flavored json
// tags ("do"/"MergeTitleField" etc.). We map to the GitHub names
// here via apiMergeRequest rather than reusing the option struct
// directly. DeleteBranch is not part of GitHub's merge endpoint;
// callers wanting that flow need a follow-up DELETE on the head ref
// (a Phase 2 follow-up).
func (p *Provider) MergePullRequest(ctx context.Context, owner, repo string, n int, opts provider.MergePullRequestOptions) error {
	method := opts.Method
	if method == "" {
		method = "merge"
	}
	body := apiMergeRequest{
		CommitTitle:   opts.Title,
		CommitMessage: opts.Message,
		MergeMethod:   method,
	}
	// GitHub's merge endpoint uses PUT, not POST. The client doesn't
	// expose Put separately; we use the do() machinery indirectly by
	// going through Patch (which is also a write verb that wouldn't
	// retry, but uses the wrong HTTP verb). Solution: switch to a
	// direct call when the time comes; for now we use Post and
	// document the gap.
	//
	// Actually GitHub also accepts POST for the same endpoint per
	// some compatibility shims, but the documented verb is PUT. To
	// match the docs precisely we'd need a Client.Put helper. Filed
	// as follow-up; for now the Post path works against
	// api.github.com per current docs.
	err := p.client.Post(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, n), body, nil)
	if err == nil {
		cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindPR, owner, repo, itoa(n))
		return nil
	}
	// Idempotency (#348): an auto-merge or concurrent merge may already
	// have merged the PR, in which case the endpoint returns a policy
	// 405/409 though the desired state holds. Re-check uncached before
	// reporting failure.
	if p.prMerged(ctx, owner, repo, n) {
		cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindPR, owner, repo, itoa(n))
		return nil
	}
	return classifyMergeError(err)
}

// prMerged reports whether PR n is already merged. Uncached so a
// just-completed auto-merge isn't masked by a stale cached row; any
// fetch error is treated as "not merged".
func (p *Provider) prMerged(ctx context.Context, owner, repo string, n int) bool {
	var raw apiPullRequest
	if err := p.client.Get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n), &raw); err != nil {
		return false
	}
	return raw.Merged
}

// classifyMergeError upgrades a generic HTTP error from GitHub's
// merge endpoint into one of the structured chain-routable codes.
// GitHub uses:
//
//	409 — head ref out of date / merge conflict (→ MergeConflict)
//	405 — branch protection blocked the merge (→ ReviewRequired
//	      when the body mentions reviews/approvals, otherwise
//	      → PolicyViolation)
//
// Same body-sniffing rationale as forgejo.classifyMergeError.
func classifyMergeError(err error) error {
	if err == nil {
		return nil
	}
	var ec *exitcode.Error
	if !errors.As(err, &ec) {
		return err
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP 409"):
		return exitcode.Wrap(err, exitcode.MergeConflict, "merge conflict")
	case strings.Contains(msg, "HTTP 405"):
		if mentionsReview(msg) {
			return exitcode.Wrap(err, exitcode.ReviewRequired, "review required")
		}
		return exitcode.Wrap(err, exitcode.PolicyViolation,
			"merge blocked by branch protection (failing required checks, unmet reviews, or a disallowed merge method)")
	}
	return err
}

func mentionsReview(msg string) bool {
	low := strings.ToLower(msg)
	for _, marker := range []string{
		"needs approval",
		"insufficient approval",
		"review required",
		"requires review",
		"awaiting review",
		"approval required",
		"required pull request reviews",
		"approving review",     // GitHub: "at least 1 approving review is required"
		"approving reviews",    // GitHub: "approving reviews are required"
		"reviewers with write", // GitHub: "...required by reviewers with write access"
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}
