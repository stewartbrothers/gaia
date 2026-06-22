package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stewartbrothers/gaia/core/provider"
	"github.com/stewartbrothers/gaia/core/types"
)

// apiCollaborator mirrors one entry in GitHub's collaborators list. Unlike
// Forgejo, GitHub supplies the permission inline: a role_name string plus a
// permissions map of booleans. role_name is the precise effective role
// ("admin", "maintain", "write", "triage", "read"); the permissions map is
// the fallback when role_name is absent on older API surfaces.
type apiCollaborator struct {
	Login       string `json:"login"`
	RoleName    string `json:"role_name"`
	Permissions struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
}

// permission returns the effective permission: role_name when present,
// otherwise the highest-privilege flag set in the permissions map. The
// derivation order (admin > maintain > push > triage > pull) mirrors
// GitHub's own role hierarchy.
func (a *apiCollaborator) permission() string {
	if a.RoleName != "" {
		return a.RoleName
	}
	switch {
	case a.Permissions.Admin:
		return "admin"
	case a.Permissions.Maintain:
		return "maintain"
	case a.Permissions.Push:
		return "push"
	case a.Permissions.Triage:
		return "triage"
	case a.Permissions.Pull:
		return "pull"
	default:
		return ""
	}
}

// ListCollaborators returns the repo's collaborator access list. GitHub
// returns a bare array with the permission inline, so no per-user resolve
// is needed (the Forgejo provider does need one).
func (p *Provider) ListCollaborators(ctx context.Context, owner, repo string, opts provider.ListCollaboratorsOptions) ([]types.Collaborator, *provider.Page, error) {
	limit := clampLimit(opts.Limit)
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("page", pageFromCursor(opts.Cursor))

	path := fmt.Sprintf("/repos/%s/%s/collaborators?%s", owner, repo, q.Encode())

	var raw []apiCollaborator
	if err := p.client.Get(ctx, path, &raw); err != nil {
		return nil, nil, err
	}
	out := make([]types.Collaborator, 0, len(raw))
	for i := range raw {
		out = append(out, types.Collaborator{
			Login:      raw[i].Login,
			Permission: raw[i].permission(),
		})
	}
	return out, makePage(len(raw), limit, opts.Cursor), nil
}
