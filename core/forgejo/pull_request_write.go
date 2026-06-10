package forgejo

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
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	if err := p.client.Post(ctx, path, opts, &raw); err != nil {
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
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n)
	if err := p.client.Patch(ctx, path, opts, &raw); err != nil {
		return nil, err
	}
	cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindPR, owner, repo, itoa(n))
	out := raw.toType()
	return &out, nil
}

// MergePullRequest performs the merge with the requested method.
//
// Forgejo's response taxonomy:
//
//	200 — merged.
//	409 — merge conflict (head ref diverged from base, can't fast-
//	      forward / rebase / squash). Mapped to exitcode.MergeConflict
//	      so chains can yield on `merge_conflict` and let the agent
//	      push a rebase commit before resuming.
//	405 — not mergeable for a policy reason. Body distinguishes:
//	         - "needs approval", "Insufficient approvals", "review"
//	             → exitcode.ReviewRequired (chain: review_required)
//	         - anything else (failing required checks, locked branch,
//	           draft PR, etc.) → exitcode.PolicyViolation (chain:
//	           policy_violation)
//	other — falls through to the generic exitcode.FromHTTP mapping.
//
// The body-sniffing for 405 is unavoidable: Forgejo returns the same
// HTTP status for "needs reviews" and "needs passing checks" and only
// the message disambiguates. We bias toward ReviewRequired only when
// the body clearly mentions reviews; everything else lands in
// PolicyViolation where the chain author can route uniformly.
func (p *Provider) MergePullRequest(ctx context.Context, owner, repo string, n int, opts provider.MergePullRequestOptions) error {
	if opts.Method == "" {
		opts.Method = "merge"
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, n)
	err := p.client.Post(ctx, path, opts, nil)
	if err == nil {
		// Merge changes state from open→merged; flush PR row + lists (#42).
		cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindPR, owner, repo, itoa(n))
		return nil
	}
	// Idempotency (#348): branch-protection auto-merge or a concurrent
	// merge may already have merged the PR, in which case the endpoint
	// returns a policy 405/409 even though the desired state holds. Before
	// reporting a (often opaque, empty-body) failure, re-check uncached;
	// if it's already merged, that's success, not an error.
	if p.prMerged(ctx, owner, repo, n) {
		cache.NewInvalidator(p.client.cache).AfterObjectMutation(ctx, kindPR, owner, repo, itoa(n))
		return nil
	}
	return classifyMergeError(err)
}

// prMerged reports whether PR n is already in the merged state. Uncached
// (p.client.Get) so a just-completed auto-merge isn't masked by a stale
// cached PR row. Any fetch error is treated as "not merged" — the caller
// then surfaces the original merge error.
func (p *Provider) prMerged(ctx context.Context, owner, repo string, n int) bool {
	var raw apiPullRequest
	if err := p.client.Get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, n), &raw); err != nil {
		return false
	}
	return raw.Merged
}

// classifyMergeError upgrades a generic HTTP error from Forgejo's
// merge endpoint into one of the structured chain-routable codes.
// Returns the input unchanged when the status doesn't match.
func classifyMergeError(err error) error {
	if err == nil {
		return nil
	}
	var ec *exitcode.Error
	if !errors.As(err, &ec) {
		return err
	}
	// statusError formats messages as "POST /...: HTTP 409: <body>" —
	// we sniff the original message to distinguish 405 vs 409 since
	// FromHTTP maps both to Generic.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "HTTP 409"):
		return exitcode.Wrap(err, exitcode.MergeConflict, "merge conflict")
	case strings.Contains(msg, "HTTP 405"):
		// 405 = "PR not mergeable for a policy reason". Sniff the body
		// to pick between ReviewRequired and PolicyViolation.
		if mentionsReview(msg) {
			return exitcode.Wrap(err, exitcode.ReviewRequired, "review required")
		}
		// Forgejo often returns an empty body here, so the wrapped cause
		// is uninformative — name the likely reasons in the message.
		return exitcode.Wrap(err, exitcode.PolicyViolation,
			"merge blocked by branch protection (failing required checks, unmet reviews, or a disallowed merge method)")
	}
	return err
}

// mentionsReview reports whether a Forgejo error body looks like a
// "this PR needs more approvals" failure rather than another policy
// block. The match list is intentionally narrow — false positives
// would push a chain author's `abort_on: [policy_violation]` to never
// fire when it should.
func mentionsReview(msg string) bool {
	low := strings.ToLower(msg)
	for _, marker := range []string{
		"needs approval",
		"insufficient approval",
		"review required",
		"requires review",
		"awaiting review",
		"approval required",
	} {
		if strings.Contains(low, marker) {
			return true
		}
	}
	return false
}
