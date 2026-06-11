package forgejo

import (
	"context"
	"fmt"

	"github.com/stewartbrothers/gaia/core/exitcode"
	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiBranchProtection mirrors the fields of Forgejo's branch-protection
// object that gaia trims into types.BranchProtection. Forgejo carries
// many more knobs (push/merge whitelists, dismiss-stale, signed
// commits); we read only the binding-relevant ones.
type apiBranchProtection struct {
	BranchName          string   `json:"branch_name"`
	EnableStatusCheck   bool     `json:"enable_status_check"`
	StatusCheckContexts []string `json:"status_check_contexts"`
	RequiredApprovals   int64    `json:"required_approvals"`
	BlockOnOutdated     bool     `json:"block_on_outdated_branch"`
}

func (a *apiBranchProtection) toType() types.BranchProtection {
	out := types.BranchProtection{
		Branch:             a.BranchName,
		StrictStatusChecks: a.BlockOnOutdated,
		RequiredApprovals:  int(a.RequiredApprovals),
	}
	if a.EnableStatusCheck {
		out.RequiredStatusChecks = append([]string(nil), a.StatusCheckContexts...)
	}
	return out
}

// bpCreatePayload is the POST body for creating a rule. Forgejo requires
// branch_name on create.
type bpCreatePayload struct {
	BranchName          string   `json:"branch_name"`
	EnableStatusCheck   bool     `json:"enable_status_check"`
	StatusCheckContexts []string `json:"status_check_contexts"`
	RequiredApprovals   int64    `json:"required_approvals"`
	BlockOnOutdated     bool     `json:"block_on_outdated_branch"`
}

// bpPatchPayload is the PATCH body for updating a rule. Pointer/explicit
// fields so the declarative set always sends the resolved value (incl.
// an empty contexts list to clear required checks).
type bpPatchPayload struct {
	EnableStatusCheck   *bool    `json:"enable_status_check,omitempty"`
	StatusCheckContexts []string `json:"status_check_contexts"`
	RequiredApprovals   *int64   `json:"required_approvals,omitempty"`
	BlockOnOutdated     *bool    `json:"block_on_outdated_branch,omitempty"`
}

func bpPath(owner, repo, branch string) string {
	return fmt.Sprintf("/repos/%s/%s/branch_protections/%s", owner, repo, branch)
}

// GetBranchProtection returns the rule for branch (NotFound when none).
func (p *Provider) GetBranchProtection(ctx context.Context, owner, repo, branch string) (*types.BranchProtection, error) {
	var raw apiBranchProtection
	if err := p.client.Get(ctx, bpPath(owner, repo, branch), &raw); err != nil {
		return nil, err
	}
	out := raw.toType()
	return &out, nil
}

// SetBranchProtection upserts the rule: PATCH when one already exists,
// POST to create otherwise. The state in opts is applied declaratively.
func (p *Provider) SetBranchProtection(ctx context.Context, owner, repo, branch string, opts provider.SetBranchProtectionOptions) (*types.BranchProtection, error) {
	contexts := opts.RequiredStatusChecks
	if contexts == nil {
		contexts = []string{} // send [] (not null) so a clear actually clears
	}
	enable := len(contexts) > 0
	approvals := int64(opts.RequiredApprovals)
	strict := opts.StrictStatusChecks

	// Does a rule already exist? Distinguish NotFound (→ create) from a
	// real error.
	_, getErr := p.GetBranchProtection(ctx, owner, repo, branch)
	switch {
	case getErr == nil:
		patch := bpPatchPayload{
			EnableStatusCheck:   &enable,
			StatusCheckContexts: contexts,
			RequiredApprovals:   &approvals,
			BlockOnOutdated:     &strict,
		}
		var raw apiBranchProtection
		if err := p.client.Patch(ctx, bpPath(owner, repo, branch), patch, &raw); err != nil {
			return nil, err
		}
		out := raw.toType()
		return &out, nil
	case exitcode.Of(getErr) == exitcode.NotFound:
		body := bpCreatePayload{
			BranchName:          branch,
			EnableStatusCheck:   enable,
			StatusCheckContexts: contexts,
			RequiredApprovals:   approvals,
			BlockOnOutdated:     strict,
		}
		var raw apiBranchProtection
		path := fmt.Sprintf("/repos/%s/%s/branch_protections", owner, repo)
		if err := p.client.Post(ctx, path, body, &raw); err != nil {
			return nil, err
		}
		out := raw.toType()
		return &out, nil
	default:
		return nil, getErr
	}
}

// DeleteBranchProtection removes the rule for branch.
func (p *Provider) DeleteBranchProtection(ctx context.Context, owner, repo, branch string) error {
	return p.client.Delete(ctx, bpPath(owner, repo, branch))
}
