// Package github: branch-protection CRUD (#350, parity follow-up to
// #345 which shipped this Forgejo-only).
//
// GitHub's branch-protection surface lives at
// `/repos/{o}/{r}/branches/{branch}/protection` and differs from
// Forgejo's in three ways gaia papers over at the trim boundary:
//
//   - The mutating verb is PUT (declarative replace) rather than
//     POST-create + PATCH-update; one call sets the whole rule, which
//     matches SetBranchProtection's "replace the rule" semantics, so
//     there's no GET-then-write dance.
//   - The PUT body is a full object: gaia must send the knobs it
//     doesn't model (enforce_admins, restrictions, the rest of
//     required_pull_request_reviews) as explicit defaults/null, or
//     GitHub 422s.
//   - On read the binding fields nest under their own objects, and the
//     required checks come back in either the legacy `contexts[]` form
//     or the newer `checks[]` ({context, app_id}) form — gaia reads
//     both. An unprotected branch 404s (→ NotFound), same as Forgejo.
package github

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiBranchProtection mirrors the fields of GitHub's branch-protection
// object that gaia trims into types.BranchProtection. GitHub carries
// many more knobs (signatures, conversation resolution, linear history,
// push restrictions); we read only the binding-relevant ones.
type apiBranchProtection struct {
	RequiredStatusChecks *apiRequiredStatusChecks `json:"required_status_checks"`
	RequiredReviews      *apiRequiredReviews      `json:"required_pull_request_reviews"`
}

type apiRequiredStatusChecks struct {
	Strict bool `json:"strict"`
	// Contexts is the legacy flat list. GitHub still populates it on
	// read for back-compat, but newer rules surface checks via Checks.
	Contexts []string `json:"contexts"`
	// Checks is the newer per-check form ({context, app_id}). gaia
	// falls back to it when Contexts is empty.
	Checks []apiStatusCheck `json:"checks"`
}

type apiStatusCheck struct {
	Context string `json:"context"`
}

type apiRequiredReviews struct {
	RequiredApprovingReviewCount int `json:"required_approving_review_count"`
}

func (a *apiBranchProtection) toType(branch string) types.BranchProtection {
	out := types.BranchProtection{Branch: branch}
	if rsc := a.RequiredStatusChecks; rsc != nil {
		out.StrictStatusChecks = rsc.Strict
		switch {
		case len(rsc.Contexts) > 0:
			out.RequiredStatusChecks = append([]string(nil), rsc.Contexts...)
		case len(rsc.Checks) > 0:
			out.RequiredStatusChecks = make([]string, 0, len(rsc.Checks))
			for _, c := range rsc.Checks {
				out.RequiredStatusChecks = append(out.RequiredStatusChecks, c.Context)
			}
		}
	}
	if rr := a.RequiredReviews; rr != nil {
		out.RequiredApprovals = rr.RequiredApprovingReviewCount
	}
	return out
}

// bpStatusChecksPayload is the required_status_checks sub-object on PUT.
type bpStatusChecksPayload struct {
	Strict   bool     `json:"strict"`
	Contexts []string `json:"contexts"`
}

// bpReviewsPayload is the required_pull_request_reviews sub-object on PUT.
type bpReviewsPayload struct {
	RequiredApprovingReviewCount int `json:"required_approving_review_count"`
}

// bpPutPayload is GitHub's full declarative-replace body. The four
// nullable fields are mandatory in the request: gaia sends the two it
// models when set and explicit null otherwise (so a clear actually
// clears), plus enforce_admins=false and restrictions=null for the
// knobs the trimmed type doesn't carry yet (#350).
type bpPutPayload struct {
	RequiredStatusChecks *bpStatusChecksPayload `json:"required_status_checks"`
	EnforceAdmins        bool                   `json:"enforce_admins"`
	RequiredReviews      *bpReviewsPayload      `json:"required_pull_request_reviews"`
	Restrictions         *struct{}              `json:"restrictions"`
}

func bpPath(owner, repo, branch string) string {
	return fmt.Sprintf("/repos/%s/%s/branches/%s/protection", owner, repo, branch)
}

// GetBranchProtection returns the rule for branch. An unprotected
// branch 404s, which maps to NotFound via the client's error mapping.
func (p *Provider) GetBranchProtection(ctx context.Context, owner, repo, branch string) (*types.BranchProtection, error) {
	var raw apiBranchProtection
	if err := p.client.Get(ctx, bpPath(owner, repo, branch), &raw); err != nil {
		return nil, err
	}
	out := raw.toType(branch)
	return &out, nil
}

// SetBranchProtection applies opts declaratively via a single PUT
// (GitHub replaces the whole rule). Unset checks/approvals go out as
// null so a "clear" actually clears; the unmodelled knobs default to
// enforce_admins=false / restrictions=null.
func (p *Provider) SetBranchProtection(ctx context.Context, owner, repo, branch string, opts provider.SetBranchProtectionOptions) (*types.BranchProtection, error) {
	body := bpPutPayload{EnforceAdmins: false, Restrictions: nil}
	if len(opts.RequiredStatusChecks) > 0 {
		body.RequiredStatusChecks = &bpStatusChecksPayload{
			Strict:   opts.StrictStatusChecks,
			Contexts: opts.RequiredStatusChecks,
		}
	}
	if opts.RequiredApprovals > 0 {
		body.RequiredReviews = &bpReviewsPayload{
			RequiredApprovingReviewCount: opts.RequiredApprovals,
		}
	}

	var raw apiBranchProtection
	if err := p.client.Put(ctx, bpPath(owner, repo, branch), body, &raw); err != nil {
		return nil, err
	}
	out := raw.toType(branch)
	return &out, nil
}

// DeleteBranchProtection removes the rule for branch (204 success).
func (p *Provider) DeleteBranchProtection(ctx context.Context, owner, repo, branch string) error {
	return p.client.Delete(ctx, bpPath(owner, repo, branch))
}
